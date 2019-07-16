package main

import (
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"testing"
)

func okHelper(t *testing.T) {
	t.Helper()

	retry.Run(t, func(r *retry.R) {
		t.Log(err)
	})

	retry.Run(t, func(r *retry.R) {
		require.NotNil(r, nil)
	})

}

func brokenHelper(t *testing.T) {
	t.Helper()

	retry.Run(t, func(r *retry.R) {
		r.Fatal(err)
	})

	t.Fatal(err)

	retry.Run(t, func(r *retry.R) {
		t.Fatal(err)
	})

}