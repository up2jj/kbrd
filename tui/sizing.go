package tui

// Size stores the latest non-negative terminal dimensions. Its zero value is
// valid, which lets controls receive sizing before or after Open.
type Size struct {
	Width  int
	Height int
}

func (s *Size) Set(width, height int) {
	s.Width = max(width, 0)
	s.Height = max(height, 0)
}

// Fit returns dimensions bounded by both the terminal and the supplied caps.
// A non-positive cap means that axis is limited only by the terminal.
func (s Size) Fit(maxWidth, maxHeight int) Size {
	width, height := s.Width, s.Height
	if maxWidth > 0 {
		width = min(width, maxWidth)
	}
	if maxHeight > 0 {
		height = min(height, maxHeight)
	}
	return Size{Width: width, Height: height}
}
