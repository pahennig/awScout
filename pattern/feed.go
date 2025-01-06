package pattern

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Patterns struct {
	Compiled map[string]*regexp.Regexp
}

func LoadPatterns(filename string) (*Patterns, error) {
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	patternStrings := make(map[string]string)
	if err := decoder.Decode(&patternStrings); err != nil {
		return nil, err
	}

	compiled := make(map[string]*regexp.Regexp)
	for name, patternStr := range patternStrings {
		pattern, err := regexp.Compile(patternStr)
		if err != nil {
			if name == "Password Pattern" {
				pattern, err = regexp.Compile(`^[A-Za-z\d]{8,}$`)
				if err != nil {
					log.Printf("Failed to compile fallback pattern for %s: %v", name, err)
					continue
				}
				log.Printf("Using fallback pattern for %s due to unsupported syntax", name)
			} else {
				log.Printf("Invalid regex pattern for %s: %v", name, err)
				continue
			}
		}
		compiled[name] = pattern
	}

	return &Patterns{Compiled: compiled}, nil
}

// - [ ] Password logic (not implemented yet)
func (p *Patterns) ValidatePassword(password string) bool {
	if pattern, ok := p.Compiled["Password Pattern"]; ok {
		if !pattern.MatchString(password) {
			return false
		}
		hasUpper := false
		hasLower := false
		hasDigit := false
		hasSpecial := false
		for _, char := range password {
			switch {
			case 'A' <= char && char <= 'Z':
				hasUpper = true
			case 'a' <= char && char <= 'z':
				hasLower = true
			case '0' <= char && char <= '9':
				hasDigit = true
			case strings.ContainsRune("!@#$%^&*", char):
				hasSpecial = true
			}
		}
		return hasUpper && hasLower && hasDigit && hasSpecial
	}
	return false
}

func (p *Patterns) MatchPatterns(userInput string, matchMode string) map[string][]string {
	matches := make(map[string][]string)
	blocklist := map[string][]string{
		"AWS_Client": {"iam:PassRole", "S3Key"},
	}

	for name, pattern := range p.Compiled {
		if name == "Password Pattern" {
			lines := regexp.MustCompile(`\r?\n`).Split(userInput, -1)
			for _, line := range lines {
				if p.ValidatePassword(line) {
					matches[name] = append(matches[name], line)
				}
			}
		} else {
			if matchMode == "FindAllStringSubmatch" {
				results := pattern.FindAllStringSubmatch(userInput, -1)
				for _, result := range results {
					matchedString := result[0]
					blocked := false
					if blocklistForPattern, exists := blocklist[name]; exists {
						for _, word := range blocklistForPattern {
							if strings.Contains(matchedString, word) {
								blocked = true
								break
							}
						}
					}
					if !blocked {
						matches[name] = append(matches[name], matchedString)
					}
				}
			} else if matchMode == "MatchString" {
				lines := regexp.MustCompile(`\r?\n`).Split(userInput, -1)
				for _, line := range lines {
					if pattern.MatchString(line) {
						blocked := false
						if blocklistForPattern, exists := blocklist[name]; exists {
							for _, word := range blocklistForPattern {
								if strings.Contains(line, word) {
									blocked = true
									break
								}
							}
						}
						if !blocked {
							matches[name] = append(matches[name], line)
						}
					}
				}
			}
		}
	}
	return matches
}
