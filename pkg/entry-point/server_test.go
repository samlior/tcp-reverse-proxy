package entry_point

import "testing"

func TestParseRoutes(t *testing.T) {
	for _, route := range []string{"22:22", "127.0.0.1:2222:22", "2222:10.0.0.123:22", "127.0.0.1:2222:10.0.0.123:22", "[::1]:2222:[2001:db8::1]:22"} {
		if _, err := ParseRoutes([]string{route}); err != nil {
			t.Errorf("valid route %q: %v", route, err)
		}
	}
	for _, route := range []string{"22", "22:localhost:22", "22:no-ip:22", "0:22", "22:0", "65536:22", "bad:22:22", "22:[::1]:22", "[::1:22:22", "[]:22:22"} {
		if _, err := ParseRoutes([]string{route}); err == nil {
			t.Errorf("invalid route %q accepted", route)
		}
	}
	if _, err := ParseRoutes([]string{"22:22", "22:23"}); err == nil {
		t.Error("duplicate listener accepted")
	}
}
