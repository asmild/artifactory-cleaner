package cleaner

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

const (
	ConfigFileName = "cleanup_queries"
	ConfigFileExt  = ".yaml"
	ConfigFile     = ConfigFileName + ConfigFileExt
)

func LoadCleanupProperties(filename string) (*CleanupProperties, error) {
	v := viper.New()
	v.SetConfigFile(filename)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	props := &CleanupProperties{}
	if err := v.Unmarshal(props); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return props, nil
}

// Warning is a non-fatal config issue found by ValidateSettings.
type Warning struct {
	Rule    string // rule name, or "" for target-level warnings
	Message string
}

// ValidateSettings checks a TargetSettings for likely config mistakes and
// returns a list of warnings. It does not abort — warnings are printed before
// the run starts so operators can fix them before the next execution.
func ValidateSettings(targetName string, s TargetSettings) []Warning {
	var warnings []Warning

	// Compile all rule patterns first; warn on invalid regexps.
	type compiledRule struct {
		RuleSettings
		re *regexp.Regexp
	}
	compiled := make([]compiledRule, 0, len(s.Rules))
	for _, rule := range s.Rules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			warnings = append(warnings, Warning{
				Rule:    rule.Name,
				Message: fmt.Sprintf("invalid pattern %q: %v", rule.Pattern, err),
			})
			continue
		}
		compiled = append(compiled, compiledRule{rule, re})
	}

	for _, rule := range compiled {
		// whitelistedVersions must match the rule's own pattern to be reachable.
		for _, v := range rule.WhitelistedVersions {
			if !rule.re.MatchString(v) {
				warnings = append(warnings, Warning{
					Rule: rule.Name,
					Message: fmt.Sprintf(
						"whitelistedVersion %q does not match pattern %q — it will never be reached by this rule",
						v, rule.Pattern,
					),
				})
			}
		}

		// Same check for the version part of whitelistedArtifacts ("group@version").
		for _, a := range rule.WhitelistedArtifacts {
			parts := strings.SplitN(a, "@", 2)
			if len(parts) == 2 && !rule.re.MatchString(parts[1]) {
				warnings = append(warnings, Warning{
					Rule: rule.Name,
					Message: fmt.Sprintf(
						"whitelistedArtifact %q version part does not match pattern %q — it will never be reached by this rule",
						a, rule.Pattern,
					),
				})
			}
		}
	}

	// protectedVersions that also match a rule pattern are redundant in the
	// whitelist — protection takes priority, but the warning helps catch
	// configs where something appears in both places.
	for _, pv := range s.ProtectedVersions {
		for _, rule := range compiled {
			if rule.re.MatchString(pv) {
				warnings = append(warnings, Warning{
					Rule: rule.Name,
					Message: fmt.Sprintf(
						"protectedVersion %q matches rule pattern %q — protection takes priority; any whitelistedVersions entry for it inside the rule is unreachable",
						pv, rule.Pattern,
					),
				})
			}
		}
	}

	return warnings
}

// PrintWarnings writes validation warnings to stdout in a readable format.
func PrintWarnings(targetName string, warnings []Warning) {
	if len(warnings) == 0 {
		return
	}
	fmt.Printf("⚠  Config warnings for target %q:\n", targetName)
	for _, w := range warnings {
		if w.Rule != "" {
			fmt.Printf("   [rule %q] %s\n", w.Rule, w.Message)
		} else {
			fmt.Printf("   [target] %s\n", w.Message)
		}
	}
	fmt.Println()
}