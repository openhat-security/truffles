package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadEnvFile reads KEY=VALUE lines into the process environment.
// Existing env vars are not overwritten. Blank lines and # comments are ignored.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNo)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key == "" {
			return fmt.Errorf("%s:%d: empty key", path, lineNo)
		}
		// Expand ${HOME} etc. inside the value at load time.
		val = os.ExpandEnv(val)
		if os.Getenv(key) == "" {
			if err := os.Setenv(key, val); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// resolveEnvFile returns path relative to the YAML file's directory when needed.
func resolveEnvFile(yamlPath, envFile string) string {
	if envFile == "" {
		return ""
	}
	if filepath.IsAbs(envFile) {
		return envFile
	}
	dir := filepath.Dir(yamlPath)
	return filepath.Join(dir, envFile)
}

func expand(s string) string {
	if s == "" {
		return s
	}
	return os.ExpandEnv(s)
}

func expandSlave(s *Slave) {
	s.Name = expand(s.Name)
	s.Host = expand(s.Host)
	s.User = expand(s.User)
	s.Key = expand(s.Key)
	s.WorkDir = expand(s.WorkDir)
	s.TrufflesPath = expand(s.TrufflesPath)
	s.GCEInstance = expand(s.GCEInstance)
	s.GCEZone = expand(s.GCEZone)
	s.GCEProject = expand(s.GCEProject)
	s.Arch = expand(s.Arch)
}

func expandCluster(c *Cluster) {
	c.Name = expand(c.Name)
	c.Master = expand(c.Master)
	c.DataDir = expand(c.DataDir)
	c.CollectOut = expand(c.CollectOut)
	for i := range c.Slaves {
		expandSlave(&c.Slaves[i])
	}
}

func expandPlaybook(pb *Playbook) {
	pb.Name = expand(pb.Name)
	pb.Description = expand(pb.Description)
	pb.Master = expand(pb.Master)
	pb.DataDir = expand(pb.DataDir)
	pb.CollectOut = expand(pb.CollectOut)
	pb.LogDir = expand(pb.LogDir)
	pb.Search.Owner = expand(pb.Search.Owner)
	pb.Search.Out = expand(pb.Search.Out)
	pb.Search.Filter = expand(pb.Search.Filter)
	pb.Search.Token = expand(pb.Search.Token)
	for i := range pb.Search.Tokens {
		pb.Search.Tokens[i] = expand(pb.Search.Tokens[i])
	}
	for i := range pb.Search.Queries {
		pb.Search.Queries[i] = expand(pb.Search.Queries[i])
	}
	pb.Scan.File = expand(pb.Scan.File)
	pb.Scan.Out = expand(pb.Scan.Out)
	pb.Scan.Token = expand(pb.Scan.Token)
	pb.Scan.SkipFile = expand(pb.Scan.SkipFile)
	pb.Scan.AppendScanned = expand(pb.Scan.AppendScanned)
	for i := range pb.Slaves {
		expandSlave(&pb.Slaves[i])
	}
}
