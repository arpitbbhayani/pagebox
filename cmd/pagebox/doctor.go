package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/daemon"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check AWS credentials, bucket access, versioning, port, hostname, daemon",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Heading("pagebox doctor", nil, nil)

		cfg, err := pbconfig.Load()
		if err != nil {
			ui.PrintErr("config check failed", err.Error(), nil)
			ui.Hint("run init to fix this:", nil)
			ui.Command("pagebox init", nil)
			os.Exit(1)
		}
		ui.PrintOk("config loaded", cfg.Backend, nil)

		if cfg.Backend == "s3" {
			checkS3(cfg)
		} else {
			ui.PrintInfo("skipping s3 checks", "backend is local", nil)
		}

		checkNetwork()
		checkDaemon()
	},
}

func checkS3(cfg pbconfig.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	awscfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		ui.PrintErr("aws credentials", "not found or invalid", nil)
		ui.Hint("configure AWS credentials to proceed:", nil)
		ui.Command("aws configure", nil)
		return
	}
	ui.PrintOk("aws credentials", "valid", nil)

	if cfg.Bucket == "" {
		ui.PrintErr("bucket name", "missing in config", nil)
		return
	}

	s3Client := s3.NewFromConfig(awscfg)
	_, err = s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: &cfg.Bucket,
	})
	
	if err != nil {
		ui.PrintErr("bucket access", fmt.Sprintf("cannot access s3://%s", cfg.Bucket), nil)
		ui.Hint("ensure your IAM policy grants s3:ListBucket, s3:GetObject, s3:PutObject, s3:DeleteObject on this bucket", nil)
	} else {
		ui.PrintOk("bucket access", fmt.Sprintf("s3://%s", cfg.Bucket), nil)
		
		// Check versioning
		ver, err := s3Client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
			Bucket: &cfg.Bucket,
		})
		if err != nil {
			ui.PrintWarn("bucket versioning", "could not check status", nil)
		} else if ver.Status != "Enabled" {
			ui.PrintWarn("bucket versioning", "is disabled", nil)
			ui.Hint("it is strongly recommended to enable versioning in S3 as a safety net against accidental overwrites", nil)
		} else {
			ui.PrintOk("bucket versioning", "enabled", nil)
		}
	}
}

func checkNetwork() {
	cfg, _ := pbconfig.Load()
	targetPort := cfg.Port
	if targetPort <= 0 {
		targetPort = 8732
	}

	// Check /etc/hosts for configured host
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	if host != "localhost" && host != "127.0.0.1" {
		b, err := os.ReadFile("/etc/hosts")
		if err != nil || !strings.Contains(string(b), host) {
			ui.PrintWarn("hostname resolution", fmt.Sprintf("%s missing from /etc/hosts", host), nil)
		} else {
			ui.PrintOk("hostname resolution", fmt.Sprintf("%s mapped", host), nil)
		}
	}

	// Check if targetPort is available (or bound by us)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", targetPort))
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			ui.PrintWarn(fmt.Sprintf("port %d", targetPort), fmt.Sprintf("unprivileged users cannot bind to port %d", targetPort), nil)
			ui.Hint("run 'pagebox daemon install' to grant access or configure an unprivileged port", nil)
		} else if strings.Contains(err.Error(), "address already in use") {
			running, _, _ := daemon.Status()
			if running {
				ui.PrintOk(fmt.Sprintf("port %d", targetPort), "bound by pagebox daemon", nil)
			} else {
				ui.PrintWarn(fmt.Sprintf("port %d", targetPort), "already in use by another process", nil)
			}
		} else {
			ui.PrintErr(fmt.Sprintf("port %d", targetPort), err.Error(), nil)
		}
	} else {
		ln.Close()
		ui.PrintOk(fmt.Sprintf("port %d", targetPort), "available to bind", nil)
	}
}

func checkDaemon() {
	running, url, err := daemon.Status()
	if err != nil {
		ui.PrintErr("daemon check", err.Error(), nil)
		return
	}
	
	if running {
		ui.PrintOk("daemon status", fmt.Sprintf("running (%s)", url), nil)
	} else {
		ui.PrintWarn("daemon status", "stopped", nil)
		ui.Hint("start it with:", nil)
		ui.Command("pagebox daemon install", nil)
	}
}
