1:{{ 'a' . $n . 'b' }}
2:{{ $s . '-' . $s }}
3:{{ 1.5 . 'x' . true . false . null . 'y' }}
4:{{ 'x' . $n == 'x2' }}
5:{{ $missing ?? 'd' . 'e' }}
6:{{ !$n . 'z' }}
7:{{ ('a' . 'b') . 'c' }}
8:{{ 'a'.'b'.$n }}
9:@if ('a' . 'b' == 'ab')
yes
@endif
10:{!! '<b>' . $s . '</b>' !!}
<x-card :title="'Hi ' . $who . '!'" class="a" />
<x-card :title="'T'" :data-x="'v-' . $n" :href="'/posts/' . $s" />
