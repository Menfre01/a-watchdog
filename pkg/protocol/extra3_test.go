package protocol

import "testing"

func TestParseJSONUsage_InvalidBody(t *testing.T) {
	u := &Usage{}
	parseJSONUsage([]byte("not-json"), u)
	if u.In != nil || u.Out != nil || u.Cache != nil {
		t.Fatalf("invalid body usage = %+v, want empty", u)
	}
}
