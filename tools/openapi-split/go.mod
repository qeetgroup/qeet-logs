// Nested module: keeps gopkg.in/yaml.v3 out of the production server's
// dependency graph. Run with `cd tools/openapi-split && go run . <mode>`.
module github.com/qeetgroup/qeet-logs/tools/openapi-split

go 1.25

require gopkg.in/yaml.v3 v3.0.1
