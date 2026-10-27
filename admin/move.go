package admin

// Move is one arrow button of an ordered list, such as the pages tree or an
// editor's ordered items. Templates show a pair with the shared
// "admin-moves" template: {{template "admin-moves" .Moves}}.
type Move struct {
	// URL is the form action that moves the item; empty shows the arrow
	// disabled, so the arrows of all rows stay aligned at the ends.
	URL string
	// Arrow is the button's symbol, such as "↑".
	Arrow string
	// Label is the translation key of the button's accessible name and
	// tooltip, such as "Move up".
	Label string
}

// Moves returns the up and down arrows of an item; an empty URL disables
// that arrow, at the first or the last place.
func Moves(up, down string) []Move {
	return []Move{
		{URL: up, Arrow: "↑", Label: "Move up"},
		{URL: down, Arrow: "↓", Label: "Move down"},
	}
}
