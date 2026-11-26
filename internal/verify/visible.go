package verify

import "github.com/openagentix/factory/internal/repo"

// suspicious and visible are the helpers of package repo, which shows
// untrusted text safely.
func suspicious(s string) bool { return repo.Suspicious(s) }

func visible(s string, max int) string { return repo.Visible(s, max) }
