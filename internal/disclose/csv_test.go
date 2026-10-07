package disclose

import (
	"strings"
	"testing"
)

func TestParseCSVAndGroup(t *testing.T) {
	in := `detector,verified,timestamp,secret,repository_url,file_line,commit,author,decoder,verification_error
URI,true,2024-01-01,https://u:p@h/,https://github.com/acme/app,a.py:1,deadbeef,a@b.c,PLAIN,
OpenWeather,false,2024-01-02,abc,https://github.com/acme/app,b.py:2,cafebabe,a@b.c,PLAIN,
URI,true,2024-01-03,x,https://github.com/other/z,c.py:3,111,a@b.c,PLAIN,
`
	rows, err := ParseCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d", len(rows))
	}
	verified := GroupByRepo(rows, true)
	if len(verified) != 2 {
		t.Fatalf("verified groups=%d want 2", len(verified))
	}
	all := GroupByRepo(rows, false)
	if len(all) != 2 {
		t.Fatalf("all groups=%d want 2", len(all))
	}
	for _, g := range all {
		if g.Key() == "acme/app" && len(g.Findings) != 2 {
			t.Fatalf("acme/app findings=%d", len(g.Findings))
		}
	}
}
