package testsuite

import (
	"context"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/publishing"
)

// TestSectionCollision checks that a module key taking the name of a section
// another module adds (publishing "news" adds "news-categories") stops the
// app with a message that says why.
func TestSectionCollision(t *testing.T) {
	t.Parallel()

	app := webthings.New(webthings.Config{StateDir: t.TempDir()})

	err := app.Use(publishing.Module("news", "News"), lists.Module("news-categories", "Categories"))
	if err != nil {
		t.Fatal(err)
	}

	err = app.Start(context.Background())
	if err == nil {
		_ = app.Close()

		t.Fatal("two sections named news-categories were accepted")
	}

	if !strings.Contains(err.Error(), `"news-categories" exists twice`) {
		t.Fatalf("unclear error: %v", err)
	}
}
