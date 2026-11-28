package app

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

const docViewFile = "../../web/src/site/apidoc.json"

// The /api reference is built from the spec as it is. Run with PROSPECTOR_WRITE_APIDOC=1
// to write it after changing api/openapi.yaml, then build web/.
func TestTheAPIReferenceIsBuiltFromTheSpec(t *testing.T) {
	t.Parallel()
	spec, err := api.Spec()
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(docViewOf(apiDocOf(spec)), "", " ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("PROSPECTOR_WRITE_APIDOC") != "" {
		if err := os.WriteFile(docViewFile, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(docViewFile)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s is out of step with the spec: run PROSPECTOR_WRITE_APIDOC=1 go test -run TestTheAPIReferenceIsBuiltFromTheSpec, then npm run build in web/", docViewFile)
	}
}

func TestTheServersAddressGoesIntoTheReference(t *testing.T) {
	t.Parallel()
	page := []byte(`<code>curl ` + docBase + `/o/your-org/api/v1/today</code>`)
	if got := string(withBase(page, `https://crm.example/"><b>`)); got != `<code>curl https://crm.example/&#34;&gt;&lt;b&gt;/o/your-org/api/v1/today</code>` {
		t.Errorf("%s", got)
	}
}
