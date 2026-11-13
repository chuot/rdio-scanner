// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"rdio-scanner/internal/app"
	"rdio-scanner/internal/application/ports"
	adminfrontend "rdio-scanner/internal/presentation/admin/frontend"
	scannerfrontend "rdio-scanner/internal/presentation/scanner/frontend"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, unbuiltBundles))
}

// unbuiltBundles answers which applications the embedded bundles do not
func unbuiltBundles() []string {
	var names []string

	if !scannerfrontend.BundleIsBuilt() {
		names = append(names, "scanner")
	}

	if !adminfrontend.BundleIsBuilt() {
		names = append(names, "admin")
	}

	return names
}

// bundleWarning is the one line the server says about bundles it cannot
func bundleWarning(names []string) string {
	apps := strings.Join(names, " and ")

	noun, verb := "application", "is not built"
	if len(names) > 1 {
		noun, verb = "applications", "are not built"
	}

	return fmt.Sprintf("warning  the %s %s %s; run go run ./cmd/build client before running", apps, noun, verb)
}

// run dispatches the modes the command line asks for and returns the exit
// code instead of calling os.Exit directly, so the modes stay testable; the
// unbuilt answer is the bundle question the applications ask of their embeds.
func run(args []string, stdout io.Writer, unbuilt func() []string) int {
	exePath, err := os.Executable()
	if err != nil {
		exePath = ""
	}

	config, mode, err := newConfig(args, exePath, stdout)
	if err != nil {
		var usage usageError
		if errors.As(err, &usage) {
			return 2
		}

		fmt.Fprintf(stdout, "error: %v\n", err)
		return 1
	}

	if mode == ModeExit {
		return 0
	}

	// the command mode drives a running server and stops, outranking the
	// administrator password mode and needing no database or
	// configuration.
	if mode == ModeCommand {
		return runCommand(config, exePath, stdout)
	}

	// the service mode's two halves: asking the manager to act ends this process,
	// and the manager's run action makes it the service with the server
	// underneath.
	if asksTheServiceManager(config.ServiceAction) {
		return controlService(config.ServiceAction)
	}

	// the maintenance mode is a one-shot that clears what a legacy migration left in
	// problem, compacts what it can, and stops: it needs the deployment open and
	// locked like the password mode, and nothing else, so it outranks it the same way.
	if config.Vacuum {
		return maintenanceRun(config, stdout)
	}

	// the administrator password mode is a one-shot that needs the database and
	// nothing else: the way a deployment sets its credential from a script, or an
	if config.NewAdminPassword != "" {
		return changeAdminPassword(config)
	}

	// a bundle that has never been built answers its pages with 404: the warning
	if names := unbuilt(); len(names) > 0 {
		fmt.Fprintln(stdout, bundleWarning(names))
	}

	return serve(config, stdout)
}

// serve starts the server and answers with it until the operator stops it.
func serve(config *Config, stdout io.Writer) int {
	// the banner is said before the database opens, so what ran is visible
	// first.
	fmt.Fprintf(stdout, "\nRdio Scanner v%s\n", Version)
	fmt.Fprintf(stdout, "----------------------------------\n")

	// under the manager, the manager's loop is handed a goroutine before the
	// database opens, so a service that cannot start is visible to the
	// manager.
	var echo app.LogEcho

	if config.ServiceAction == serviceActionRun {
		d, err := newDaemon()
		if err != nil {
			return exitWithError(err)
		}

		d.run()

		echo = app.NewServiceLog(d.logger)
	}

	srv, err := app.New(serverConfig(config, os.Stderr, echo))
	if err != nil {
		return exitWithError(err)
	}

	if err := srv.Start(context.Background()); err != nil {
		srv.Close()

		return exitWithError(err)
	}

	err = srv.Serve(context.Background())

	srv.Close()

	if err != nil {
		return exitWithError(err)
	}

	return 0
}

// serverConfig is the configuration the composition root builds a server
// from: the effective configuration, plus what this program decides on its
// own way of running: where the server says what it says out loud, whose
func serverConfig(config *Config, out io.Writer, echo app.LogEcho) app.Config {
	return app.Config{
		BaseDir:     config.BaseDir,
		DbType:      config.DbType,
		DbFile:      config.DbFile,
		DbHost:      config.DbHost,
		DbPort:      config.DbPort,
		DbName:      config.DbName,
		DbUsername:  config.DbUsername,
		DbPassword:  config.DbPassword,
		Listen:      config.Listen,
		SslAutoCert: config.SslAutoCert,
		SslCertFile: config.SslCertFile,
		SslKeyFile:  config.SslKeyFile,
		SslListen:   config.SslListen,

		// the two switches the operator opts in to are carried across by hand and
		// not by the fields that share a name: a switch the command layer parses and
		TrustedProxies: config.TrustedProxies,
		McpServer:      config.McpServer,

		Version: Version,
		Out:     out,
		Echo:    echo,
		Converter: func(dir string) (ports.AudioConverter, ports.AudioLengthProber, string) {
			ffmpegPath, ffprobePath, failure := bundledFFmpeg(dir)
			choice := theConverter(ffmpegPath, ffprobePath, failure)

			return choice.converter(), choice.prober(dir), choice.Notice
		},
	}
}

// maintenanceRun is the -vacuum mode: open the deployment the way the server does,
// so the in-use refusal covers it, clear the records a legacy migration left in
// problem, compact the file when the backend is a sqlite one the server can rewrite,
// say what each half did, and stop without serving. The report-only switch reads the
// same listing and stops there, because the operator asked what a run would do rather
// than for it to be done.
func maintenanceRun(config *Config, stdout io.Writer) int {
	ctx := context.Background()

	srv, err := app.New(serverConfig(config, os.Stderr, nil))
	if err != nil {
		if errors.Is(err, app.ErrInUse) {
			return exitWithError(errors.New("the database is in use — stop the server before vacuuming"))
		}

		return exitWithError(err)
	}

	fmt.Fprintln(stdout, maintenanceHeader(config))

	summary, err := maintenanceSummary(ctx, srv, config)
	if err != nil {
		srv.Close()

		return exitWithError(err)
	}

	if err := srv.Close(); err != nil {
		return exitWithError(err)
	}

	for _, line := range summary.Lines {
		fmt.Fprintln(stdout, line)
	}

	// a backend the server cannot rewrite is named as the half that did not run,
	// and a report-only run names neither half because the operator asked for
	// neither.
	if summary.CompactionSkip != "" {
		fmt.Fprintln(stdout, summary.CompactionSkip)

		return 0
	}

	if config.VacuumDryRun {
		return 0
	}

	reclaimed := (summary.Before.FreelistCount - summary.After.FreelistCount) * summary.Before.PageSize

	fmt.Fprintf(stdout, "vacuumed: %d free pages, %d free pages, %d bytes reclaimed\n",
		summary.Before.FreelistCount, summary.After.FreelistCount, reclaimed)

	return 0
}

// maintenanceSummary asks the composition root for the half of the run the operator
// asked for: the clearing with the compaction that follows it, or the listing that
// stands in for both.
func maintenanceSummary(ctx context.Context, srv *app.Server, config *Config) (app.MaintenanceSummary, error) {
	if config.VacuumDryRun {
		return srv.MaintainReportOnly(ctx)
	}

	return srv.Maintain(ctx)
}

// maintenanceHeader names the deployment the run works on, in the words that fit the
// backend it works on and the half the operator asked for. A sqlite deployment is
// named by the file the run rewrites; a database the server connects to is named by
// its type, because the run has no file of its own to point at.
func maintenanceHeader(config *Config) string {
	if config.DbType != DbTypeSqlite {
		if config.VacuumDryRun {
			return fmt.Sprintf("reporting on the %s database", config.DbType)
		}

		return fmt.Sprintf("clearing the %s database", config.DbType)
	}

	if config.VacuumDryRun {
		return fmt.Sprintf("reporting on %s", config.GetDbFilePath())
	}

	return fmt.Sprintf("vacuuming %s", config.GetDbFilePath())
}

// changeAdminPassword is the -admin_password mode: store the credential the
// administrator chose, say so in the log view, and stop without starting a
func changeAdminPassword(config *Config) int {
	ctx := context.Background()

	srv, err := app.New(serverConfig(config, os.Stderr, nil))
	if err != nil {
		return exitWithError(err)
	}

	if err := srv.ChangeAdminPassword(ctx, config.NewAdminPassword); err != nil {
		srv.Close()

		return exitWithError(err)
	}

	if err := srv.Close(); err != nil {
		return exitWithError(err)
	}

	return 0
}

// exitWithError prints the failure on the standard error and ends with a
// code.
func exitWithError(err error) int {
	log.Println(err)

	return 1
}

// usageError marks a command-line parsing failure, which maps to exit code 2.
type usageError struct {
	err error
}

func (e usageError) Error() string {
	return e.err.Error()
}

func (e usageError) Unwrap() error {
	return e.err
}

var _ error = usageError{}
