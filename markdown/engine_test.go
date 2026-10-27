package markdown

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestConcurrentRender checks that renders sharing the one engine at the
// same time each resolve media with their own options.
func TestConcurrentRender(t *testing.T) {
	source := "# Title\n\n![](media:0190a8f2-7c1e-7b3a-9d4e-1a2b3c4d5e6f) and [a file](media:0190a8f2-7c1e-7b3a-9d4e-1a2b3c4d5e6f)"

	var group sync.WaitGroup

	for i := range 50 {
		group.Go(func() {
			want := fmt.Sprintf("/files/%d/", i)
			html := string(Render(source, Options{Media: func(id string) string {
				return want + id
			}}))

			if strings.Count(html, want) != 2 || !strings.Contains(html, `<h1 id="title">`) {
				t.Errorf("render %d: %s", i, html)
			}
		})
	}

	group.Wait()
}
