package gositter_test

import (
	"fmt"
	"strings"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/python"
)

func Example() {
	src := []byte("def add(a, b):\n    return a + b\n")

	p := gositter.NewParser()
	if err := p.SetLanguage(python.Language()); err != nil {
		panic(err)
	}
	tree := p.Parse(src)

	var walk func(n gositter.Node, depth int)
	walk = func(n gositter.Node, depth int) {
		fmt.Printf("%s%s [%d, %d)\n", strings.Repeat("  ", depth), n.Kind(), n.StartByte(), n.EndByte())
		for _, c := range n.NamedChildren(nil) {
			walk(c, depth+1)
		}
	}
	walk(tree.RootNode(), 0)
	// Output:
	// module [0, 32)
	//   function_definition [0, 31)
	//     identifier [4, 7)
	//     parameters [7, 13)
	//       identifier [8, 9)
	//       identifier [11, 12)
	//     block [19, 31)
	//       return_statement [19, 31)
	//         binary_operator [26, 31)
	//           identifier [26, 27)
	//           identifier [30, 31)
}
