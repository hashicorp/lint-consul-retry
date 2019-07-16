# lint-consul-retry
Checks if `consul/sdk/testutil/retry.Run` uses `testing.T`.

`retry.Run` needs to operate on `retry.R` rather than `testing.T`, else the function will not retry on errors.

Examples:

```go
require := require.New(t)

retry.Run(t, func(r *retry.R) {
  require.NotNil(err)
}
```

```go
retry.Run(t, func(r *retry.R) {
  require.NotNil(t, err)
}
```

```go
retry.Run(t, func(r *retry.R) {
  if err := myFunc(); err != nil {
    t.Fatalf("failing")
   }
}
```

### Usage:
Run `./lint-consul-retry` from the base directory of Consul.
