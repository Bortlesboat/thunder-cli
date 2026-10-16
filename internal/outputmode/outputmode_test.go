package outputmode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "absent", args: []string{"status"}, want: false},
		{name: "bare flag", args: []string{"status", "--json"}, want: true},
		{name: "explicit true", args: []string{"--json=true", "status"}, want: true},
		{name: "explicit false", args: []string{"status", "--json=false"}, want: false},
		{name: "last value wins", args: []string{"--json", "--json=false"}, want: false},
		{name: "invalid value requests JSON error", args: []string{"--json=invalid"}, want: true},
		{name: "after argument separator", args: []string{"status", "--", "--json"}, want: false},
		{name: "different flag prefix", args: []string{"--json-output"}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, JSONRequested(test.args))
		})
	}
}
