package config

import "testing"

func TestNormalizePrimaryPath(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "/login", want: "/login"},
		{in: "login", want: "/login"},
		{in: "/login/", want: "/login"},
		{in: "  /x7f2  ", want: "/x7f2"},
		{in: "/secret/entry", want: "/secret/entry"},
		{in: "", wantErr: true},
		{in: "/", wantErr: true},
		{in: "/l", wantErr: true},
		{in: "/l/abc", wantErr: true},
		{in: "/a b", wantErr: true},
		{in: "/a?b", wantErr: true},
	}
	for _, c := range cases {
		got, err := normalizePrimaryPath(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizePrimaryPath(%q): expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizePrimaryPath(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizePrimaryPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
