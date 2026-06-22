package datago

import "testing"

func TestSmokeCommandQuotesArgsWithSpaces(t *testing.T) {
	spec := Spec{
		ID: "999",
		Smoke: &Smoke{
			Operation: "목록 조회",
			Params: map[string]string{
				"AREA": "서울 중구",
				"PAGE": "1",
			},
		},
	}
	got := spec.SmokeCommand()
	want := `datapan get 999 --operation "목록 조회" "AREA=서울 중구" PAGE=1 --json`
	if got != want {
		t.Fatalf("SmokeCommand()=%q want %q", got, want)
	}
}

func TestCommandStringLeavesSimpleArgsUnquoted(t *testing.T) {
	got := CommandString([]string{"datapan", "get", "15126469", "LAWD_CD=11110", "--json"})
	want := "datapan get 15126469 LAWD_CD=11110 --json"
	if got != want {
		t.Fatalf("CommandString()=%q want %q", got, want)
	}
}
