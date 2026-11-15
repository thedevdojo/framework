package htmlscan

import "testing"

// TestExpr pins the friendly-expression rewrite: what an author writes inside
// {{ … }} or a tag attribute in an .html file, and the Blade-form expression
// the shared evaluator (blade/expr) receives.
func TestExpr(t *testing.T) {
	cases := []struct{ in, want string }{
		// variables and member access
		{`title`, `$title`},
		{`post.title`, `$post->title`},
		{`site.nav.items`, `$site->nav->items`},
		{`posts[0].title`, `$posts[0]->title`},
		{`loop.first`, `$loop->first`},
		{`  title  `, `  $title  `},
		{``, ``},

		// equality: =, == and === all mean "equals"
		{`name == 'Tony'`, `$name == 'Tony'`},
		{`name = 'Tony'`, `$name == 'Tony'`},
		{`name='Tony'`, `$name=='Tony'`},
		{`name === 'Tony'`, `$name == 'Tony'`},
		{`name != 'Tony'`, `$name != 'Tony'`},
		{`name !== 'Tony'`, `$name != 'Tony'`},

		// ordering operators pass through untouched
		{`count >= 3`, `$count >= 3`},
		{`count <= 3`, `$count <= 3`},
		{`a < b`, `$a < $b`},
		{`a > b`, `$a > $b`},
		{`price > 1.5`, `$price > 1.5`},

		// word operators
		{`a and b`, `$a && $b`},
		{`a or b`, `$a || $b`},
		{`not a`, `! $a`},
		{`a AND not b`, `$a && ! $b`},
		{`a && b || !c`, `$a && $b || !$c`},

		// literals are never variables
		{`active == true`, `$active == true`},
		{`x == null`, `$x == null`},
		{`y == FALSE`, `$y == FALSE`},
		{`tagline ?? 'Welcome'`, `$tagline ?? 'Welcome'`},

		// string contents are left alone
		{`title == 'a.b and c = d'`, `$title == 'a.b and c = d'`},
		{`title == "it's not"`, `$title == "it's not"`},
		{`title == 'it\'s'`, `$title == 'it\'s'`},

		// a member is never rewritten, even when it reads like a keyword
		{`post.not`, `$post->not`},
		{`post.and.or`, `$post->and->or`},
		{`post.true`, `$post->true`},

		// Blade-form expressions pass straight through
		{`$post->title`, `$post->title`},
		{`$a == $b`, `$a == $b`},
		{`$post.title`, `$post->title`},

		// arrays, method calls on the attributes bag, function calls
		{`['a', b]`, `['a', $b]`},
		{`['k' => v]`, `['k' => $v]`},
		{`attributes.merge(['class' => 'btn'])`, `$attributes->merge(['class' => 'btn'])`},
		{`attributes.class(['btn', 'on' => active])`, `$attributes->class(['btn', 'on' => $active])`},
		{`count(posts)`, `count($posts)`},

		// a dot that is not member access stays a dot (the evaluator then
		// reports concatenation, exactly as it does for Blade)
		{`'a' . b`, `'a' . $b`},
		{`a . b`, `$a . $b`},

		// malformed input is passed through for the evaluator to diagnose
		{`'unterminated`, `'unterminated`},
		{`a ? b : c`, `$a ? $b : $c`},
	}
	for _, tc := range cases {
		if got := Expr(tc.in); got != tc.want {
			t.Errorf("Expr(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}
