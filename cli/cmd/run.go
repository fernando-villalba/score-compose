/*
Apache Score
Copyright 2020 The Apache Software Foundation

This product includes software developed at
The Apache Software Foundation (http://www.apache.org/).
*/
package cmd

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/compose-spec/compose-go/types"
	"github.com/imdario/mergo"
	"github.com/spf13/cobra"

	"github.com/score-spec/score-compose/internal/compose"

	loader "github.com/score-spec/score-go/loader"
	score "github.com/score-spec/score-go/types"
)

const (
	scoreFileDefault     = "./score.yaml"
	overridesFileDefault = "./overrides.score.yaml"
)

var (
	scoreFile     string
	overridesFile string
	outFile       string
	envFile       string
	buildCtx      string

	verbose bool
)

func init() {
	runCmd.Flags().StringVarP(&scoreFile, "file", "f", scoreFileDefault, "Source SCORE file")
	runCmd.Flags().StringVar(&overridesFile, "overrides", overridesFileDefault, "Overrides SCORE file")
	runCmd.Flags().StringVarP(&outFile, "output", "o", "", "Output file")
	runCmd.Flags().StringVar(&envFile, "env-file", "", "Location to store sample .env file")
	runCmd.Flags().StringVar(&buildCtx, "build", "", "Replaces 'image' name with compose 'build' instruction")

	runCmd.Flags().BoolVar(&verbose, "verbose", false, "Enable diagnostic messages (written to STDERR)")

	rootCmd.AddCommand(runCmd)
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Translate the SCORE file to docker-compose configuration",
	RunE:  run,
}

func run(cmd *cobra.Command, args []string) error {
	if !verbose {
		log.SetOutput(io.Discard)
	}

	// Open source file
	//
	log.Printf("Reading '%s'...\n", scoreFile)
	var err error
	var src *os.File
	if src, err = os.Open(scoreFile); err != nil {
		return err
	}
	defer src.Close()

	// Parse SCORE spec
	//
	log.Print("Parsing SCORE spec...\n")
	var srcMap map[string]interface{}
	if err = loader.ParseYAML(&srcMap, src); err != nil {
		return err
	}

	// Apply overrides (optional)
	//
	if overridesFile != "" {
		log.Printf("Checking '%s'...\n", overridesFile)
		if ovr, err := os.Open(overridesFile); err == nil {
			defer ovr.Close()

			log.Print("Applying SCORE overrides...\n")
			var ovrMap map[string]interface{}
			if err = loader.ParseYAML(&ovrMap, ovr); err != nil {
				return err
			}
			if err := mergo.MergeWithOverwrite(&srcMap, ovrMap); err != nil {
				return fmt.Errorf("applying overrides fom '%s': %w", overridesFile, err)
			}
		} else if !os.IsNotExist(err) || overridesFile != overridesFileDefault {
			return err
		}
	}

	// Validate SCORE spec
	//
	log.Print("Validating SCORE spec...\n")
	var spec score.WorkloadSpec
	if err = loader.MapSpec(&spec, srcMap); err != nil {
		return fmt.Errorf("validating workload spec: %w", err)
	}

	// Build docker-compose configuration
	//
	log.Print("Building docker-compose configuration...\n")
	proj, vars, err := compose.ConvertSpec(&spec)
	if err != nil {
		return fmt.Errorf("building docker-compose configuration: %w", err)
	}

	// Override 'image' reference with 'build' instructions
	//
	if buildCtx != "" {
		log.Printf("Applying build instructions: '%s'...\n", buildCtx)
		for idx := range proj.Services {
			if proj.Services[idx].Name == spec.Metadata.Name {
				proj.Services[idx].Build = &types.BuildConfig{Context: buildCtx}
				proj.Services[idx].Image = ""
			}
		}
	}

	// Open output file (optional)
	//
	var dest = io.Writer(os.Stdout)
	if outFile != "" {
		log.Printf("Creating '%s'...\n", outFile)
		destFile, err := os.Create(outFile)
		if err != nil {
			return err
		}
		defer destFile.Close()

		dest = io.MultiWriter(dest, destFile)
	}

	// Write docker-compose spec
	//
	log.Print("Writing docker-compose configuration...\n")
	if err = compose.WriteYAML(dest, proj); err != nil {
		return err
	}

	if envFile != "" {
		// Open .env file
		//
		log.Printf("Creating '%s'...\n", envFile)
		dest, err := os.Create(envFile)
		if err != nil {
			return err
		}
		defer dest.Close()

		// Write .env file
		//
		log.Print("Writing .env file template...\n")
		for key, val := range vars {
			if val == nil {
				val = ""
			}
			var envVar = fmt.Sprintf("%s=%v\n", key, val)
			if _, err := dest.WriteString(envVar); err != nil {
				return err
			}
		}
	}

	return nil
}
