package main

import (
	"context"
	"fmt"
	"os"
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

		cfg := pbconfig.DefaultConfig()

		backend, err := ui.Prompt("storage mode [s3 / local]", "s3")
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
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			
			awscfg, err := config.LoadDefaultConfig(ctx)
			if err != nil {
				s.Stop(false)
				ui.PrintErr("could not load AWS config", err.Error(), nil)
				ui.Hint("configure AWS credentials to proceed:", nil)
				ui.Command("aws configure", nil)
				os.Exit(1)
			}
			
			stsClient := sts.NewFromConfig(awscfg)
			_, err = stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
			if err != nil {
				s.Stop(false)
				ui.PrintErr("invalid AWS credentials", err.Error(), nil)
				ui.Hint("ensure your credentials are valid and have access:", nil)
				ui.Command("aws sts get-caller-identity", nil)
				os.Exit(1)
			}
			s.Stop(true)
			ui.PrintOk("aws credentials active", "", nil)

			bucket, err := ui.Prompt("S3 bucket name", "")
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
			_, err = s3Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket})
			s2.Stop(true)

			if err != nil {
				// We don't have access or it doesn't exist. Ask to create.
				create, cerr := ui.Confirm(fmt.Sprintf("Bucket '%s' not found or not accessible. Create it now?", bucket), true)
				if cerr != nil {
					os.Exit(1)
				}
				if create {
					sp3 := ui.NewSpinner(fmt.Sprintf("Creating bucket %s", bucket))
					_, cerr := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
						Bucket: &bucket,
						CreateBucketConfiguration: &types.CreateBucketConfiguration{
							LocationConstraint: types.BucketLocationConstraint(awscfg.Region),
						},
					})
					sp3.Stop(true)
					if cerr != nil {
						// Some regions (us-east-1) don't like LocationConstraint. Let's try without it if it fails.
						if strings.Contains(cerr.Error(), "LocationConstraint") || strings.Contains(cerr.Error(), "IllegalLocationConstraintException") {
							sp4 := ui.NewSpinner(fmt.Sprintf("Retrying bucket creation %s", bucket))
							_, cerr = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket})
							sp4.Stop(true)
						}
					}
					
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
			
			prefix, _ := ui.Prompt("S3 prefix (leave empty for root)", "")
			cfg.Prefix = prefix
		}

		docsDir, _ := ui.Prompt("docs directory", cfg.DocsDir)
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
		ui.Hint("try next:", nil)
		ui.Command("pagebox daemon install", nil)
		ui.Command("pagebox serve", nil)
	},
}
