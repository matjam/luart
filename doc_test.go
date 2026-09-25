package luart_test

import (
	"fmt"

	"github.com/matjam/luart"
)

// averageAndSum receives a variable number of numerical arguments and returns their average and sum.
func averageAndSum(l *luart.State) int {
	n := l.Top() // Number of arguments.
	var sum float64
	for i := 1; i <= n; i++ {
		f, ok := l.ToNumber(i)
		if !ok {
			l.PushString("incorrect argument")
			l.Error()
		}
		sum += f
	}
	l.PushNumber(sum / float64(n)) // First result.
	l.PushNumber(sum)              // Second result.
	return 2                       // Result count.
}

func ExampleFunction() {
	l := luart.NewState()
	l.Register("averageAndSum", averageAndSum)
	l.Global("averageAndSum")
	l.PushNumber(2)
	l.PushNumber(4)
	l.Call(2, 2)
	avg, _ := l.ToNumber(-2)
	sum, _ := l.ToNumber(-1)
	fmt.Println(avg, sum)
	// Output: 3 6
}
