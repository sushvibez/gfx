// JS-subset scripting: `function` declarations with var/for/if/return, used
// from live bindings and signal handlers. Edit live; it hot-reloads on save.
import QtQuick 2.15

Column {
    padding: 20; spacing: 14
    color: "#131318"
    property int n: 7

    // Iterative Fibonacci — var, for, return.
    function fib(k) {
        if (k < 2) return k
        var a = 0
        var b = 1
        for (var i = 2; i <= k; i = i + 1) {
            var t = a + b
            a = b
            b = t
        }
        return b
    }

    // FizzBuzz classifier — chained ifs and string returns.
    function classify(x) {
        if (x % 15 == 0) return "fizzbuzz"
        if (x % 3 == 0) return "fizz"
        if (x % 5 == 0) return "buzz"
        return "" + x
    }

    // Array indexing + .length + compound/increment operators.
    function arraySum(json) {
        var a = JSON.parse(json)
        var total = 0
        for (var i = 0; i < a.length; i++) total += a[i]
        return total
    }

    Text { text: "JS functions & control flow"; color: "#f1f1f7" }

    Row {
        spacing: 10; items: "center"
        Button { text: "n--"; width: 70; onClicked: { if (n > 0) n = n - 1 } }
        Button { text: "n++"; width: 70; onClicked: n = n + 1 }
        Text { text: "n = " + n; color: "#e8e8f0" }
    }

    Rectangle {
        padding: 16; spacing: 8
        color: "#1d1d27"; radius: 12
        border.width: 1; border.color: "#32323f"
        Text { text: "fib(n)      = " + fib(n); color: "#91a7ff" }
        Text { text: "classify(n) = " + classify(n); color: "#69db7c" }
        Text { text: "max(n,10)   = " + Math.max(n, 10); color: "#ffd43b" }
        Text { text: "sum[3,7,9]  = " + arraySum("[3, 7, 9]"); color: "#ff922b" }
    }
}
