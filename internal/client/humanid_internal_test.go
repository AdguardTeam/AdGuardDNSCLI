package client

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func FuzzFQDNToHumanID(f *testing.F) {
	for _, seed := range []string{
		"",
		"f",
		"foo",
		"foo-bar",
		"foo--bar",
		"foo.",
		"foo.bar",
		"foo.bar.",
		".",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		id, err := fqdnToHumanID(input)
		if err != nil {
			require.Empty(t, id)

			return
		}

		require.NotEmpty(t, id)
		require.NotContains(t, string(id), ".")

		_, err = newHumanID(string(id))
		require.NoError(t, err)
	})
}
