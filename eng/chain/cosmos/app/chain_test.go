package app_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"nexus/app"
	"nexus/lib"
)

type ChainTestSuite struct {
	suite.Suite
}

func (s *ChainTestSuite) writeConfigFile(contents string) {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "config.yaml")

	err := os.WriteFile(path, []byte(contents), 0644)
	s.Require().NoError(err)

	s.T().Setenv(lib.NEXUS_CONFIG_PATH, path)
}

func (s *ChainTestSuite) TestEmptyConfig() {
	chainSpec := app.LoadChainSpec()
	s.Require().Nil(chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithFork() {
	contents := `forks: { prague_timestamp: 1 }`
	s.writeConfigFile(contents)
	chainSpec := app.LoadChainSpec()
	s.Require().NotNil(chainSpec.PragueTimestamp)
	s.Require().Equal(uint64(1), *chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithInvalidFork() {
	contents := `forks: { abcd: 1 }`
	s.writeConfigFile(contents)
	chainSpec := app.LoadChainSpec()
	s.Require().Nil(chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithPragueTimestampZero() {
	contents := `forks:
  prague_timestamp: 0`
	s.writeConfigFile(contents)
	chainSpec := app.LoadChainSpec()
	s.Require().NotNil(chainSpec.PragueTimestamp)
	s.Require().Equal(uint64(0), *chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithPragueTimestampLarge() {
	contents := `forks:
  prague_timestamp: 1000000`
	s.writeConfigFile(contents)
	chainSpec := app.LoadChainSpec()
	s.Require().NotNil(chainSpec.PragueTimestamp)
	s.Require().Equal(uint64(1000000), *chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithMultiLineYAML() {
	contents := `forks:
  prague_timestamp: 42`
	s.writeConfigFile(contents)
	chainSpec := app.LoadChainSpec()
	s.Require().NotNil(chainSpec.PragueTimestamp)
	s.Require().Equal(uint64(42), *chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithMissingForksSection() {
	contents := `other_field: value`
	s.writeConfigFile(contents)
	// Missing forks section results in empty ChainSpec (PragueTimestamp is nil)
	chainSpec := app.LoadChainSpec()
	s.Require().Nil(chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithEmptyForksSection() {
	contents := `forks: {}`
	s.writeConfigFile(contents)
	chainSpec := app.LoadChainSpec()
	s.Require().Nil(chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithInvalidYAML() {
	contents := `forks: [invalid: yaml`
	s.writeConfigFile(contents)
	s.Require().Panics(func() {
		app.LoadChainSpec()
	})
}

func (s *ChainTestSuite) TestConfigWithNonNumericPragueTimestamp() {
	contents := `forks:
  prague_timestamp: "not_a_number"`
	s.writeConfigFile(contents)
	s.Require().Panics(func() {
		app.LoadChainSpec()
	})
}

func (s *ChainTestSuite) TestConfigWithNegativePragueTimestamp() {
	contents := `forks:
  prague_timestamp: -1`
	s.writeConfigFile(contents)
	// YAML will parse -1 as int64, but uint64 conversion might fail or wrap
	// Let's see what happens - it might panic or convert incorrectly
	s.Require().Panics(func() {
		app.LoadChainSpec()
	})
}

func (s *ChainTestSuite) TestConfigWithAdditionalFields() {
	contents := `forks:
  prague_timestamp: 100
other_section:
  some_field: value`
	s.writeConfigFile(contents)
	chainSpec := app.LoadChainSpec()
	s.Require().NotNil(chainSpec.PragueTimestamp)
	s.Require().Equal(uint64(100), *chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithMissingConfigFile() {
	// Unset the config path to simulate missing file
	s.T().Setenv(lib.NEXUS_CONFIG_PATH, "")
	chainSpec := app.LoadChainSpec()
	s.Require().Nil(chainSpec.PragueTimestamp)
}

func (s *ChainTestSuite) TestConfigWithPragueTimestampAsString() {
	contents := `forks:
  prague_timestamp: "100"`
	s.writeConfigFile(contents)
	// YAML cannot unmarshal string into uint64, should panic
	s.Require().Panics(func() {
		app.LoadChainSpec()
	})
}

func (s *ChainTestSuite) TestConfigWithPragueAndOsakaTimestamp() {
	contents := `forks:
  prague_timestamp: 10
  osaka_timestamp: 20`
	s.writeConfigFile(contents)
	chainSpec := app.LoadChainSpec()
	s.Require().NotNil(chainSpec.PragueTimestamp)
	s.Require().Equal(uint64(10), *chainSpec.PragueTimestamp)
	s.Require().NotNil(chainSpec.OsakaTimestamp)
	s.Require().Equal(uint64(20), *chainSpec.OsakaTimestamp)
}

func (s *ChainTestSuite) TestConfigWithOsakaTimestampWithoutPraguePanics() {
	contents := `forks:
  osaka_timestamp: 50`
	s.writeConfigFile(contents)
	// ChainSpec.Validate requires prague_timestamp when osaka_timestamp is set (fork order).
	s.Require().Panics(func() {
		app.LoadChainSpec()
	})
}

func TestChainSuite(t *testing.T) {
	suite.Run(t, new(ChainTestSuite))
}
