# lint-consul-retry
Checks if function literal in `consul/sdk/testutil/retry.Run` uses `t *testing.T`.

`retry.Run` needs to operate on `retry.R` rather than `testing.T`, else the function will not retry on errors.

### Examples:
#### Bad:
```go
require := require.New(t)

retry.Run(t, func(r *retry.R) {
  require.NotNil(err)
}
```

```go
retry.Run(t, func(r *retry.R) {
  assert.NotNil(t, err)
}
```

```go
retry.Run(t, func(r *retry.R) {
  if err := myFunc(); err != nil {
    t.Fatalf("failing")
   }
}
```

#### OK:

```go
retry.Run(t, func(r *retry.R) {
  require.NotNil(r, err)
}
```

```go
retry.Run(t, func(t *retry.R) {
  assert.NotNil(t, err)
}
```

```go
retry.Run(t, func(r *retry.R) {
  if err := myFunc(); err != nil {
    r.Fatalf("failing")
   }
}
```

### Usage:
Run `./lint-consul-retry` from the base directory of Consul.
