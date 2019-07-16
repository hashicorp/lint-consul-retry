package main

import (
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"testing"
)

func TestAPI_HealthNode(t *testing.T) {
	retry.Run(t, func(r *retry.R) {
		r.Fatal(err)
	})

	t.Fatal(err)

	retry.Run(t, func(r *retry.R) {
		r.Fatal(err)
	})

}

func TestAPI_Broken(t *testing.T) {
	retry.Run(t, func(r *retry.R) {
		t.Fatal(err)
	})

	assert.NoErr(t, err)
}

func TestAPI_AlsoBroken(t *testing.T) {
	retry.Run(t, func(r *retry.R) {
		assert.NoErr(t, err)
	})
}

func TestAPI_SuperBroken(t *testing.T) {
	require := require.New(t)
	retry.Run(t, func(r *retry.R) {
		assert.NoErr(err)
	})
}

func TestAPI_Fine(t *testing.T) {
	retry.Run(t, func(r *retry.R) {
		require := require.New(r)
		assert.NoErr(err)
	})
}