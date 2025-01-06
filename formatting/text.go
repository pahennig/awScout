package formatting

import (
	"strings"

	"github.com/fatih/color"
)

var (
	resourceNameColor = color.New(color.FgCyan).Add(color.Bold)
	patternNameColor  = color.New(color.FgGreen).Add(color.Bold)
	matchedDataColor  = color.New(color.FgYellow)
	detailsColor      = color.New(color.FgHiCyan)
)

func Title(name string, resource string) {
	resourceNameColor.Printf("%s: %s\n", name, resource)
}

func Data(name string, data string) {
	detailsColor.Printf("%s: %s\n", name, data)
}

func PatterName(patternName string) {
	patternNameColor.Printf("Pattern: %s\n", patternName)
}

func CloudformationParameter() {
	resourceNameColor.Printf("Parameters:\n")
}

func FuncCodeDetails(section string, matches map[string][]string, showContent bool) {
	for patternName, matchedStrings := range matches {
		patternNameColor.Printf("Pattern: %s\n", patternName)
		for _, match := range matchedStrings {
			if showContent {
				if len(match) > 150 {
					match = match[:147] + "..."
				}
				matchedDataColor.Printf("Matched Data: %s\n", strings.TrimSpace(match))
			} else {
				anonymized := Anonymize(match)
				if len(anonymized) > 150 {
					anonymized = anonymized[:147] + "..."
				}
				matchedDataColor.Printf("Matched Data:%s\n", strings.TrimSpace(anonymized))
			}
		}
	}
}

func LambdaFunctionData(functionName string, version string) {
	resourceNameColor.Printf("Lambda Function: %s (Version: %s)\n", functionName, version)
}

func Content(content string, show bool) {
	if show {
		if len(content) > 150 {
			content = content[:147] + "..."
		}
		matchedDataColor.Printf("Matched Data: %s\n", strings.TrimSpace(content))
	} else {
		anonymized := Anonymize(content)
		if len(anonymized) > 150 {
			anonymized = anonymized[:147] + "..."
		}
		matchedDataColor.Printf("Matched Data: %s\n", strings.TrimSpace(anonymized))
	}
}
