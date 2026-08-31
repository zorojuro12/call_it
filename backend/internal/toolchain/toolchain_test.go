package toolchain

import (
	"strings"
	"testing"
)

func TestParseGoDirective(t *testing.T) {
	tests := []struct {
		name    string
		gomod   string
		want    string
		wantErr string
	}{
		{
			name:  "simple directive",
			gomod: "module example.com/x\n\ngo 1.22.10\n\nrequire (\n)\n",
			want:  "1.22.10",
		},
		{
			name:  "major minor only",
			gomod: "module example.com/x\n\ngo 1.26\n",
			want:  "1.26",
		},
		{
			name:    "no directive",
			gomod:   "module example.com/x\n",
			wantErr: "no go directive",
		},
		{
			name:    "commented out directive only",
			gomod:   "module x\n// go 1.99\n",
			wantErr: "no go directive",
		},
		{
			name:  "trailing comment stripped",
			gomod: "module x\ngo 1.26.7 // toolchain floor\n",
			want:  "1.26.7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGoDirective(tt.gomod)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseGoDirective() err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseGoDirective() unexpected err = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseGoDirective() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCIPin(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    []string
		wantErr string
	}{
		{
			name: "single pin",
			yaml: "jobs:\n  backend:\n    steps:\n      - uses: actions/setup-go@v5\n        with:\n          go-version: \"1.22\"\n",
			want: []string{"1.22"},
		},
		{
			name: "two pins",
			yaml: "jobs:\n  backend:\n    steps:\n      - go-version: \"1.26\"\n  frontend-e2e:\n    steps:\n      - go-version: \"1.26\"\n",
			want: []string{"1.26", "1.26"},
		},
		{
			name: "unquoted pin",
			yaml: "jobs:\n  backend:\n    steps:\n      - go-version: 1.26\n",
			want: []string{"1.26"},
		},
		{
			name:    "no pin",
			yaml:    "jobs:\n  backend:\n    steps:\n      - uses: actions/checkout@v4\n",
			wantErr: "no go-version pin",
		},
		{
			name:    "node-version must not match",
			yaml:    "jobs:\n  frontend:\n    steps:\n      - node-version: \"24\"\n",
			wantErr: "no go-version pin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCIPin(tt.yaml)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseCIPin() err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCIPin() unexpected err = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseCIPin() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ParseCIPin()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMajorMinor(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"1.26.7", "1.26"},
		{"1.26", "1.26"},
		{"1.22.10", "1.22"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := MajorMinor(tt.in); got != tt.want {
			t.Fatalf("MajorMinor(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

