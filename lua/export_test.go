package lua

// openLibraries opens the standard libraries for the tests in this
// package. They live in package stdlib, which imports this one, so these
// tests cannot import it; libs_test.go, an external test file in the same
// test binary, sets it with SetTestLibraries before any test runs.
var openLibraries func(*State)

// SetTestLibraries sets the function the package's own tests use to open
// the standard libraries.
func SetTestLibraries(open func(*State)) { openLibraries = open }
