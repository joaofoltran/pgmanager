package backup

import (
	"fmt"
	"strings"
)

const defaultConfigPath = "/etc/pgbackrest/pgbackrest.conf"

func GenerateConfig(stanzas []StanzaConfig) string {
	var b strings.Builder

	b.WriteString("[global]\n")
	b.WriteString("repo1-retention-full=2\n")
	b.WriteString("repo1-retention-diff=3\n")
	b.WriteString("compress-type=zst\n")
	b.WriteString("compress-level=3\n")
	b.WriteString("log-level-console=info\n")
	b.WriteString("log-level-file=detail\n")
	b.WriteString("start-fast=y\n")
	b.WriteString("stop-auto=y\n")
	b.WriteString("delta=y\n")

	for _, sc := range stanzas {
		b.WriteString(fmt.Sprintf("\n[%s]\n", sc.Name))
		b.WriteString(fmt.Sprintf("pg1-path=%s\n", sc.PGPath))
		b.WriteString(fmt.Sprintf("pg1-port=%d\n", sc.PGPort))
		if sc.PGUser != "" {
			b.WriteString(fmt.Sprintf("pg1-user=%s\n", sc.PGUser))
		}
		b.WriteString(fmt.Sprintf("repo1-path=%s\n", sc.RepoPath))

		if sc.RetainFull > 0 {
			b.WriteString(fmt.Sprintf("repo1-retention-full=%d\n", sc.RetainFull))
		}
		if sc.RetainDiff > 0 {
			b.WriteString(fmt.Sprintf("repo1-retention-diff=%d\n", sc.RetainDiff))
		}
		if sc.Compress != "" {
			b.WriteString(fmt.Sprintf("compress-type=%s\n", sc.Compress))
		}
	}

	return b.String()
}

func StanzaNameForCluster(clusterID string) string {
	r := strings.NewReplacer(" ", "-", "_", "-")
	return r.Replace(strings.ToLower(clusterID))
}
