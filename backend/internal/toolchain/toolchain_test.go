package toolchain

import (
	"os"
	"path/filepath"
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

func TestToolchainPinsMeetFloorAndAgree(t *testing.T) {
	gomodPath := filepath.Join("..", "..", "go.mod")
	gomodBytes, err := os.ReadFile(gomodPath)
	if err != nil {
		t.Fatalf("reading %s: %v", gomodPath, err)
	}

	ciPath := filepath.Join("..", "..", "..", ".github", "workflows", "ci.yml")
	ciBytes, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatalf("reading %s: %v", ciPath, err)
	}

	goDirective, err := ParseGoDirective(string(gomodBytes))
	if err != nil {
		t.Fatalf("ParseGoDirective: %v", err)
	}
	goMM := MajorMinor(goDirective)

	if compareMajorMinor(goMM, MinGo) < 0 {
		t.Fatalf("go.mod directive %q (major.minor %q) is below the MinGo floor %q", goDirective, goMM, MinGo)
	}

	pins, err := ParseCIPin(string(ciBytes))
	if err != nil {
		t.Fatalf("ParseCIPin: %v", err)
	}
	if len(pins) < 2 {
		t.Fatalf("expected at least two go-version pins in ci.yml, got %d: %v", len(pins), pins)
	}

	for _, pin := range pins {
		if pin != goMM {
			t.Fatalf("CI go-version pin %q does not match go.mod's major.minor %q", pin, goMM)
		}
	}
}

// compareMajorMinor compares two "X.Y" version strings component-wise as
// integers. Returns <0, 0, or >0.
func compareMajorMinor(a, b string) int {
	pa := strings.SplitN(a, ".", 2)
	pb := strings.SplitN(b, ".", 2)
	for i := 0; i < 2; i++ {
		na, nb := 0, 0
		if i < len(pa) {
			na = atoiSafe(pa[i])
		}
		if i < len(pb) {
			nb = atoiSafe(pb[i])
		}
		if na != nb {
			return na - nb
		}
	}
	return 0
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
