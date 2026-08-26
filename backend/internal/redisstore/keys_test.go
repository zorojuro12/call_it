package redisstore

import "testing"

func TestKeys(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"RoomKey", RoomKey("r1"), "room:r1"},
		{"RoomWalletsKey", RoomWalletsKey("r1"), "room:r1:wallets"},
		{"RoomCodeKey", RoomCodeKey("WXYZ"), "code:WXYZ"},
		{"RoomRoundKey", RoomRoundKey("rm1"), "room:rm1:round"},
		{"RoomOpeningKey", RoomOpeningKey("rm1"), "room:rm1:opening"},
		{"RoundKey", RoundKey("n1"), "round:n1"},
		{"RoundPoolsKey", RoundPoolsKey("n1"), "round:n1:pools"},
		{"RoundWagersKey", RoundWagersKey("n1"), "round:n1:wagers"},
		{"RoundBettorsKey", RoundBettorsKey("n1"), "round:n1:bettors"},
		{"IdemKey", IdemKey("abc"), "idem:abc"},
		{"WagerField", WagerField("u1", 2), "u1:2"},
		{"UserKey", UserKey("u1"), "user:u1"},
		{"EmailKey", EmailKey("a@b.c"), "email:a@b.c"},
		{"RateLimitKey", RateLimitKey("auth", "1.2.3.4"), "ratelimit:auth:1.2.3.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestParseWagerField(t *testing.T) {
	tests := []struct {
		name        string
		field       string
		wantUserID  string
		wantOutcome int
		wantErr     bool
	}{
		{
			name:        "simple user id and outcome",
			field:       "u1:2",
			wantUserID:  "u1",
			wantOutcome: 2,
		},
		{
			name:        "UUID user id",
			field:       "11111111-2222-3333-4444-555555555555:0",
			wantUserID:  "11111111-2222-3333-4444-555555555555",
			wantOutcome: 0,
		},
		{
			name:    "no colon",
			field:   "nocolon",
			wantErr: true,
		},
		{
			name:    "non-integer outcome",
			field:   "u1:notanint",
			wantErr: true,
		},
		{
			name:    "empty outcome",
			field:   "u1:",
			wantErr: true,
		},
		{
			name:        "user id containing a colon splits on the last colon",
			field:       "guest:u1:2",
			wantUserID:  "guest:u1",
			wantOutcome: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID, outcome, err := ParseWagerField(tt.field)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseWagerField(%q): got nil error, want an error", tt.field)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseWagerField(%q): unexpected error: %v", tt.field, err)
			}
			if userID != tt.wantUserID || outcome != tt.wantOutcome {
				t.Errorf("ParseWagerField(%q) = (%q, %d), want (%q, %d)", tt.field, userID, outcome, tt.wantUserID, tt.wantOutcome)
			}
		})
	}
}
