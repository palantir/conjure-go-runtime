module github.com/palantir/conjure-go-runtime/conjure-go-client/useragent/testmain

go 1.15

require (
	// This version will be reported in the tests, even though we override to the local path.
	github.com/palantir/conjure-go-runtime/v2 v2.2.0
	github.com/stretchr/testify v1.4.0
)

replace github.com/palantir/conjure-go-runtime/v2 => ../../../
