// Package starter writes a new site from a template on GitHub: what
// `devdojo new` does. The starter is github.com/devdojo-templates/starter; any
// other public GitHub repository that holds a site can be named instead.
//
// A template is downloaded as GitHub packs it (a gzipped tar of the default
// branch), unpacked into a folder of its own beside the target, checked, and
// only then moved into place. So a download that fails, or a template that
// is not a site, leaves nothing behind.
//
// The archive is not trusted. A name that leaves the folder, a link of either
// kind and a device are each refused, the whole download is capped in size,
// and what only a template's listing needs (template.json, thumbnail.png, the
// template's README, the repository's .git files) is left out.
package starter

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultOwner is where a template named by itself ("blog") is looked for.
const DefaultOwner = "devdojo-templates"

// DefaultTemplate is the template `devdojo new` writes when none is named.
const DefaultTemplate = "starter"

// DefaultBaseURL is where GitHub hands out a repository's download.
const DefaultBaseURL = "https://codeload.github.com"

// BaseURLEnv names an environment variable that, when set, is used instead of
// DefaultBaseURL. The tests point it at a server of their own.
const BaseURLEnv = "DEVDOJO_TEMPLATES_URL"

// DefaultMaxBytes caps how much a template may unpack to. The starter is
// under 5 MB; a template a hundred times that is not a website.
const DefaultMaxBytes = 200 << 20

// maxEntries caps how many files and folders a template may have.
const maxEntries = 50000

// ErrNotEmpty is returned by Write for a folder that already has something in
// it. Nothing is downloaded and nothing is written in that case.
var ErrNotEmpty = errors.New("the folder is not empty")

// Template is a repository on GitHub that holds a site.
type Template struct {
	Owner, Repo string
	// Asked is the name as the person gave it, for messages.
	Asked string
}

// String is the template's address as a person would write it.
func (t Template) String() string { return "github.com/" + t.Owner + "/" + t.Repo }

// githubName is what GitHub allows in an owner or repository name, near
// enough: letters, digits, '-', '_' and '.'.
var githubName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Parse reads a template's name: nothing (the starter), a bare name ("blog",
// a template of devdojo-templates), "owner/repo", or the https address of a
// repository on GitHub.
func Parse(name string) (Template, error) {
	asked := name
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultTemplate
	}
	rest, isURL := strings.CutPrefix(name, "https://github.com/")
	if !isURL {
		rest, isURL = strings.CutPrefix(name, "github.com/")
	}
	if !isURL && strings.Contains(name, "://") {
		return Template{}, fmt.Errorf("%q is not a GitHub repository. Name a template (blog), a repository (owner/repo), or its address (https://github.com/owner/repo)", asked)
	}
	if isURL {
		rest = strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
	}
	parts := strings.Split(rest, "/")
	if isURL && len(parts) != 2 {
		return Template{}, fmt.Errorf("%q is not the address of a repository: use https://github.com/owner/repo", asked)
	}
	if len(parts) == 1 {
		parts = []string{DefaultOwner, parts[0]}
	}
	if len(parts) != 2 {
		return Template{}, fmt.Errorf("%q is not a template name. Name a template (blog), a repository (owner/repo), or its address (https://github.com/owner/repo)", asked)
	}
	for _, p := range parts {
		if !githubName.MatchString(p) || strings.Trim(p, ".") == "" || strings.HasPrefix(p, "-") {
			return Template{}, fmt.Errorf("%q is not a template name. Name a template (blog), a repository (owner/repo), or its address (https://github.com/owner/repo)", asked)
		}
	}
	if asked == "" {
		asked = DefaultTemplate
	}
	return Template{Owner: parts[0], Repo: parts[1], Asked: asked}, nil
}

// Fetcher downloads templates. The zero value downloads from GitHub, or from
// $DEVDOJO_TEMPLATES_URL when that is set.
type Fetcher struct {
	// BaseURL is where downloads are asked for: BaseURL/owner/repo/tar.gz/HEAD.
	BaseURL string
	// Client makes the request; one with a generous timeout when nil.
	Client *http.Client
	// MaxBytes caps what a template unpacks to; DefaultMaxBytes when zero.
	MaxBytes int64
}

func (f Fetcher) baseURL() string {
	if f.BaseURL != "" {
		return strings.TrimSuffix(f.BaseURL, "/")
	}
	if env := os.Getenv(BaseURLEnv); env != "" {
		return strings.TrimSuffix(env, "/")
	}
	return DefaultBaseURL
}

func (f Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (f Fetcher) maxBytes() int64 {
	if f.MaxBytes > 0 {
		return f.MaxBytes
	}
	return DefaultMaxBytes
}

// Write downloads the template and writes it into dir, and reports how many
// files it wrote. The folder is made when it does not exist; one that exists
// must be empty, and is checked before anything is downloaded. On any failure
// nothing is left: not the files, not a folder Write made.
func (f Fetcher) Write(t Template, dir string) (int, error) {
	// Absolute, so the folder the template is unpacked into first is always
	// beside dir and never inside it (`devdojo new .`).
	dir, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	if err := checkTarget(dir); err != nil {
		return 0, err
	}

	resp, err := f.client().Get(f.baseURL() + "/" + t.Owner + "/" + t.Repo + "/tar.gz/HEAD")
	if err != nil {
		return 0, fmt.Errorf("couldn't reach GitHub to download %s. Check your internet connection and try again", t)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return 0, fmt.Errorf("there is no template called %q at %s", t.Asked, t)
	case resp.StatusCode != http.StatusOK:
		return 0, fmt.Errorf("GitHub did not hand over %s (it answered %s). Try again in a minute", t, resp.Status)
	}

	// The folders above dir that do not exist yet are made now, and taken
	// away again if anything after this fails.
	made, err := makeParents(dir)
	if err != nil {
		return 0, err
	}
	fail := func(err error) (int, error) {
		if made != "" {
			os.RemoveAll(made)
		}
		return 0, err
	}

	tmp, err := os.MkdirTemp(filepath.Dir(filepath.Clean(dir)), ".devdojo-new-")
	if err != nil {
		return fail(err)
	}
	count, err := unpack(resp.Body, tmp, t, f.maxBytes())
	if err == nil {
		err = isSite(tmp, t)
	}
	if err == nil {
		err = moveInto(tmp, dir)
	}
	os.RemoveAll(tmp)
	if err != nil {
		return fail(err)
	}
	return count, nil
}

// CheckFolder says whether dir may be written: it does not exist, or it is an
// empty folder. Write asks it first too; it is here for a caller that wants
// the answer before it says it is downloading.
func CheckFolder(dir string) error { return checkTarget(dir) }

// checkTarget says whether dir may be written: it does not exist, or it is an
// empty folder.
func checkTarget(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case err == nil && !info.IsDir():
		return fmt.Errorf("%s is a file, not a folder", dir)
	case err == nil:
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return ErrNotEmpty
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	return nil
}

// makeParents makes the folders above dir and returns the highest one it
// made, or "" when they were all there.
func makeParents(dir string) (string, error) {
	parent := filepath.Dir(filepath.Clean(dir))
	highest := ""
	for p := parent; ; p = filepath.Dir(p) {
		if _, err := os.Stat(p); err == nil {
			break
		}
		highest = p
		if filepath.Dir(p) == p {
			break
		}
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	return highest, nil
}

// errTooBig is returned for a template that unpacks to more than the cap.
type errTooBig struct{ max int64 }

func (e errTooBig) Error() string {
	return fmt.Sprintf("the template is bigger than %d MB, which is more than devdojo new will download", e.max>>20)
}

// unpack writes the archive's files into dir, without the archive's top
// folder and without what only a template's listing needs.
func unpack(body io.Reader, dir string, t Template, max int64) (int, error) {
	notArchive := func(why string) error {
		return fmt.Errorf("what came back from %s is not a template download (%s)", t, why)
	}
	gz, err := gzip.NewReader(body)
	if err != nil {
		return 0, notArchive("it is not a gzip file")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	top, count, entries := "", 0, 0
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, notArchive(err.Error())
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue // GitHub's note of the commit the download is of
		}
		if entries++; entries > maxEntries {
			return 0, fmt.Errorf("the template has more than %d files, which is more than devdojo new will write", maxEntries)
		}

		name := strings.TrimPrefix(hdr.Name, "./")
		first, rel, _ := strings.Cut(strings.TrimSuffix(name, "/"), "/")
		if strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") || climbs(name) || first == "" {
			return 0, fmt.Errorf("the template has a file that is outside the template's folder (%s), so nothing was written", hdr.Name)
		}
		if top == "" {
			top = first
		} else if first != top {
			return 0, notArchive("its files are not all in one folder")
		}
		if rel == "" || leaveOut(rel) {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, dir+string(filepath.Separator)) {
			return 0, fmt.Errorf("the template has a file that is outside the template's folder (%s), so nothing was written", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return 0, err
			}
		case tar.TypeReg:
			if total += hdr.Size; total > max {
				return 0, errTooBig{max}
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return 0, err
			}
			mode := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				mode = 0o755
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return 0, err
			}
			_, err = io.Copy(out, tr)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return 0, notArchive(err.Error())
			}
			count++
		case tar.TypeSymlink, tar.TypeLink:
			return 0, fmt.Errorf("the template's %s is a link, which devdojo new does not write, so nothing was written", rel)
		default:
			return 0, fmt.Errorf("the template's %s is not a file or a folder, so nothing was written", rel)
		}
	}
	if top == "" {
		return 0, notArchive("it is empty")
	}
	return count, nil
}

// climbs reports whether a name in the archive has a ".." in it anywhere.
func climbs(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." {
			return true
		}
	}
	return path.IsAbs(name)
}

// leaveOut reports whether a file of the template stays behind. At the top:
// the listing's template.json and thumbnail.png and the template's README.
// Anywhere: the Finder's .DS_Store and the repository's own .git files, but
// not .gitignore (the site wants it) or .gitkeep (it holds a folder open).
func leaveOut(rel string) bool {
	segments := strings.Split(rel, "/")
	if len(segments) == 1 {
		switch segments[0] {
		case "template.json", "thumbnail.png", "README.md":
			return true
		}
	}
	for _, s := range segments {
		if s == ".DS_Store" {
			return true
		}
		if strings.HasPrefix(s, ".git") && s != ".gitignore" && s != ".gitkeep" {
			return true
		}
	}
	return false
}

// isSite says whether an unpacked template is a site: its source in src/, or
// in resources/ for a template in the older shape.
func isSite(dir string, t Template) error {
	for _, source := range []string{"src", "resources"} {
		if info, err := os.Stat(filepath.Join(dir, source)); err == nil && info.IsDir() {
			return nil
		}
	}
	return fmt.Errorf("%s is not a site: it has no src/ or resources/ folder", t)
}

// moveInto puts the unpacked template where it was asked for. A folder that
// does not exist is the unpacked one, renamed. An empty one that does gets
// each thing moved into it, and has those taken away again if one move fails.
func moveInto(tmp, dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(tmp, dir)
	}
	// Asked again: something may have been put there while the template was
	// downloading, and a move must never write over it.
	if err := checkTarget(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return err
	}
	var moved []string
	for _, e := range entries {
		to := filepath.Join(dir, e.Name())
		if err := os.Rename(filepath.Join(tmp, e.Name()), to); err != nil {
			for _, m := range moved {
				os.RemoveAll(m)
			}
			return err
		}
		moved = append(moved, to)
	}
	return nil
}
