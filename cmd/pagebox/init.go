package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/spf13/cobra"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create the store and config",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Heading("pagebox init", nil, os.Stdout)

		cfg, err := pbconfig.Load()
		if err != nil {
			cfg = pbconfig.DefaultConfig()
		}

		backendDefault := cfg.Backend
		if backendDefault == "" {
			backendDefault = "s3"
		}

		backend, err := ui.Prompt("storage mode [s3 / local]", backendDefault)
		if err != nil {
			ui.PrintErr("cancelled", err.Error(), nil)
			os.Exit(0)
		}
		
		backend = strings.ToLower(backend)
		if backend != "s3" && backend != "local" {
			ui.PrintErr("invalid backend", "must be 's3' or 'local'", nil)
			os.Exit(1)
		}
		
		cfg.Backend = backend

		if cfg.Backend == "s3" {
			// Check AWS credentials
			s := ui.NewSpinner("Checking AWS credentials")
			authCtx, authCancel := context.WithTimeout(context.Background(), 10*time.Second)
			var opts []func(*config.LoadOptions) error
			if cfg.Region != "" {
				opts = append(opts, config.WithRegion(cfg.Region))
			}
			if cfg.AWSProfile != "" {
				opts = append(opts, config.WithSharedConfigProfile(cfg.AWSProfile))
			}
			awscfg, err := config.LoadDefaultConfig(authCtx, opts...)
			if err != nil {
				authCancel()
				s.Stop(false)
				ui.PrintErr("could not load AWS config", err.Error(), nil)
				ui.Hint("configure AWS credentials to proceed:", nil)
				ui.Command("aws configure", nil)
				os.Exit(1)
			}
			
			stsClient := sts.NewFromConfig(awscfg)
			_, err = stsClient.GetCallerIdentity(authCtx, &sts.GetCallerIdentityInput{})
			authCancel()
			if err != nil {
				s.Stop(false)
				ui.PrintErr("invalid AWS credentials", err.Error(), nil)
				ui.Hint("ensure your credentials are valid and have access:", nil)
				ui.Command("aws sts get-caller-identity", nil)
				os.Exit(1)
			}
			s.Stop(true)
			ui.PrintOk("aws credentials active", "", nil)

			if awscfg.Region != "" {
				cfg.Region = awscfg.Region
			}

			bucket, err := ui.Prompt("S3 bucket name", cfg.Bucket)
			if err != nil || bucket == "" {
				if err == nil {
					ui.PrintErr("cancelled", "bucket name is required for s3 backend", nil)
				}
				os.Exit(1)
			}
			cfg.Bucket = bucket

			// check if bucket exists
			s2 := ui.NewSpinner(fmt.Sprintf("Checking bucket %s", bucket))
			s3Client := s3.NewFromConfig(awscfg)
			headCtx, headCancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err = s3Client.HeadBucket(headCtx, &s3.HeadBucketInput{Bucket: &bucket})
			headCancel()
			s2.Stop(err == nil)

			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || (headCtx.Err() == context.DeadlineExceeded) {
					ui.PrintErr("connection timed out checking bucket", err.Error(), nil)
					os.Exit(1)
				}

				// We don't have access or it doesn't exist. Ask to create.
				create, cerr := ui.Confirm(fmt.Sprintf("Bucket '%s' not found or not accessible. Create it now?", bucket), true)
				if cerr != nil {
					os.Exit(1)
				}
				if create {
					sp3 := ui.NewSpinner(fmt.Sprintf("Creating bucket %s", bucket))
					createCtx, createCancel := context.WithTimeout(context.Background(), 30*time.Second)

					createInput := &s3.CreateBucketInput{
						Bucket: &bucket,
					}
					if awscfg.Region != "" && awscfg.Region != "us-east-1" {
						createInput.CreateBucketConfiguration = &types.CreateBucketConfiguration{
							LocationConstraint: types.BucketLocationConstraint(awscfg.Region),
						}
					}

					_, cerr := s3Client.CreateBucket(createCtx, createInput)
					createCancel()

					if cerr != nil {
						// Some regions (us-east-1) don't like LocationConstraint. Let's try without it if it fails.
						if strings.Contains(cerr.Error(), "LocationConstraint") || strings.Contains(cerr.Error(), "IllegalLocationConstraintException") {
							sp4 := ui.NewSpinner(fmt.Sprintf("Retrying bucket creation %s", bucket))
							retryCtx, retryCancel := context.WithTimeout(context.Background(), 30*time.Second)
							_, cerr = s3Client.CreateBucket(retryCtx, &s3.CreateBucketInput{Bucket: &bucket})
							retryCancel()
							sp4.Stop(cerr == nil)
						}
					}
					
					sp3.Stop(cerr == nil)
					if cerr != nil {
						ui.PrintErr("failed to create bucket", cerr.Error(), nil)
						os.Exit(1)
					}
					ui.PrintOk("bucket created", bucket, nil)
				} else {
					ui.PrintWarn("proceeding without creating bucket", "pagebox will fail if it cannot access this bucket later", nil)
				}
			} else {
				ui.PrintOk("bucket exists", bucket, nil)
			}
			
			cfg.Prefix = ""
		}

		docsDirDefault := cfg.DocsDir
		if docsDirDefault == "" {
			home, _ := os.UserHomeDir()
			docsDirDefault = filepath.Join(home, "pagebox")
		}
		docsDir, _ := ui.Prompt("docs directory", docsDirDefault)
		if docsDir == "" {
			docsDir = docsDirDefault
		}
		cfg.DocsDir = docsDir

		// Create store and save config
		s4 := ui.NewSpinner("Saving configuration")
		err = pbconfig.Save(cfg)
		s4.Stop(true)
		if err != nil {
			ui.PrintErr("failed to save config", err.Error(), nil)
			os.Exit(1)
		}
		
		err = os.MkdirAll(cfg.DocsDir, 0755)
		if err != nil {
			ui.PrintWarn("could not create docs dir", err.Error(), nil)
		}

		ui.PrintOk("pagebox initialized", "", nil)
		ui.Hint("choose how to run pagebox:", nil)
		ui.Bullet("run as a background service (recommended):", nil)
		ui.Command("pagebox daemon install", nil)
		ui.Bullet("or run in foreground (current terminal):", nil)
		ui.Command("pagebox serve", nil)
	},
}
