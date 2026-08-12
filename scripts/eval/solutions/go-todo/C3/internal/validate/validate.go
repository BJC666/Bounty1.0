package validate

import "fmt"

const MaxTitleLength = 100

// ValidateTitle checks that a todo title is usable.
func ValidateTitle(title string) error {
	if len([]rune(title)) == 0 {
		return fmt.Errorf("title must not be empty")
	}
	if len([]rune(title)) > MaxTitleLength {
		return fmt.Errorf("title too long: max %d characters", MaxTitleLength)
	}
	return nil
}
