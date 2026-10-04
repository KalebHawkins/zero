Captured output of `go test -json ./...`, used by check_test.go.

pass.json            go1.26.3, the hello-world solution
fail.json            go1.26.3, the hello-world starter
compile-error.json   go1.26.3, hello-world with hello.go returning an undefined name
subtests.json        go1.26.3, a small package with failing subtests, a skipped test,
                     a multi-line message, a panic, and a test that never ran because
                     of the panic. The folder name in the panic trace was replaced
                     with /home/learner/zero/calc.

compile-error-go122.json and .stderr are written by hand, not captured. They have the
shape Go 1.22 and 1.23 use for a compile error: the compiler text goes to standard
error and standard output carries only the "[build failed]" line.
