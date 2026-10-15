package transition

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/cloud"
	"github.com/cccteam/ccc/impulse/app"
	cloudrundeclaration "github.com/cccteam/ccc/resource/jobs/cloudrun/declaration"
)

// starterWiring is the wiring every scheduled job needs, which the first of them brings
// and the rest find in place: the rpc package and its Client carrying the job process's
// starter, the method marked @schedule, the site level's scheduler guard and job driver,
// their exposure on the App, the test configurers' fakes, the generator's WithRPC, and
// the development template's documentation of the driver's variable. What the
// application wired already is left as it is.
type starterWiring struct {
	// method is the scheduled method the wiring is made for, named in the notes.
	method string
	// command is the job process's command the method starts (cmd/jobs <command>).
	command string
	// withStore gives the test configurers a memory file store beside the fakes: the
	// file store's transition builds every suite's resource client over one.
	withStore bool
}

// wire makes the wiring: the method in the rpc package (and the package where the
// application has none), the site level, the App, the test configurers, the generator
// program, and the template.
func (w starterWiring) wire(a *app.App, s *scaffold, g *app.Generator, ch *Change) error {
	rpcDir, hasClient, err := w.writeMethod(a, s, g, ch)
	if err != nil {
		return err
	}
	if err := w.editSiteConfig(a, ch); err != nil {
		return err
	}
	if err := w.editApp(a, s.modulePath, rpcDir, hasClient, ch); err != nil {
		return err
	}
	if err := w.editHarnesses(a, ch); err != nil {
		return err
	}
	if err := w.editProgram(a, g, rpcDir, ch); err != nil {
		return err
	}

	return w.documentTemplate(a, ch)
}

// documentTemplate documents the job driver's variable in the development template,
// unset, where the template does not name it yet.
func (starterWiring) documentTemplate(a *app.App, ch *Change) error {
	if a.EnvTemplate == "" {
		return nil
	}
	data, mode, err := readFile(a, a.EnvTemplate)
	if err != nil {
		return err
	}
	text, documented := documentJobsTemplate(string(data))
	if !documented {
		return nil
	}
	if err := os.WriteFile(a.Abs(a.EnvTemplate), []byte(text), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: %s documented in the site block, unset", a.EnvTemplate, jobsTemplateVariable)

	return nil
}

// jobsTemplateVariable is the job driver's variable (cloudrun.Settings), which the site
// level declares by embedding the settings, so the development template documents it.
// The name is read off the driver's declaration, by its template field, so the template
// documents the variable the driver reads however the driver spells it.
var jobsTemplateVariable = templateVariableOf(cloudrundeclaration.Settings())

// jobsTemplateField is the field of the job driver's settings struct that carries the
// template job, by the name the declaration gives it.
const jobsTemplateField = "Template"

// templateVariableOf reads the template variable off the job driver's declaration: the
// variable the tag of its template field names, without the tag's options. Empty when
// the declaration has no field of that name, which the tests hold it against.
func templateVariableOf(d cloud.Declaration) string {
	for _, f := range d.Fields {
		if f.Name == jobsTemplateField {
			variable, _, _ := strings.Cut(f.Tag, ",")

			return strings.TrimSpace(variable)
		}
	}

	return ""
}

// jobsTemplateBlock documents the job driver's variable in the development template,
// unset: no job is configured in development.
var jobsTemplateBlock = "# " + jobsTemplateVariable + " is the job process's template job as the Cloud Run API names it, read by\n" +
	"# the job driver (resource/jobs/cloudrun), whose settings the site level embeds; on Cloud Run\n" +
	"# the stack sets it, and the site starts the copy of its own build. Unset, as in development,\n" +
	"# no job is configured, and a scheduled method that starts one says so.\n" +
	"# export " + jobsTemplateVariable + "=\n"

// documentJobsTemplate adds the job driver's variable to the template's site block,
// after the block's heading, or at the end when the template has no site block; a
// template that names the variable already is left as it is. It reports whether the
// block was added.
func documentJobsTemplate(text string) (string, bool) {
	if strings.Contains(text, jobsTemplateVariable) {
		return text, false
	}
	at := strings.Index(text, envTemplateSiteSep)
	if at < 0 || (at > 0 && text[at-1] != '\n') {
		return strings.TrimRight(text, "\n") + "\n\n" + jobsTemplateBlock, true
	}
	end := strings.Index(text[at:], "\n")
	if end < 0 {
		return text + "\n" + jobsTemplateBlock, true
	}
	end += at + 1

	return text[:end] + jobsTemplateBlock + text[end:], true
}

// writeMethod writes the scheduled method into the application's rpc package, with the
// package and its Client where the application has none: an application with an rpc
// package already keeps its Client, and giving it the job process's starter is the
// agent's unless the Client carries one (Jobs()). It returns the rpc package's directory
// and whether the Client carries the starter, so the App's wiring knows what it can
// construct.
func (w starterWiring) writeMethod(a *app.App, s *scaffold, g *app.Generator, ch *Change) (rpcDir string, hasClient bool, err error) {
	rpcDir = g.RPCDir()
	fresh := rpcDir == ""
	if fresh {
		rpcDir = rpcDirDefault
		if exists(a, rpcDir) {
			ch.skipf("%s exists but the generator program declares no WithRPC over it, so the scheduled method was not written; declare WithRPC(%q) and write %s (@rpc, @schedule(%q)) there, starting %s through the job process's starter", rpcDir, rpcDir, w.method, s.schedule, w.command)

			return "", false, nil
		}
	}
	pkg := path.Base(rpcDir)
	if !fresh {
		pkg = packageNameOf(a, rpcDir)
	}
	methodFile := path.Join(rpcDir, s.names.methodFile)
	if exists(a, methodFile) {
		ch.skipf("%s exists, so the scheduled method was not written; make it start %s through the job process's starter on the schedule %q", methodFile, w.command, s.schedule)

		return rpcDir, false, nil
	}
	if fresh {
		if err := writeGo(a, path.Join(rpcDir, rpcClientFile), w.rpcClientSource(pkg)); err != nil {
			return "", false, err
		}
	}
	if err := writeGo(a, methodFile, s.kind.method(s, pkg)); err != nil {
		return "", false, err
	}
	if fresh {
		ch.didf("%s: the rpc package, its Client carrying the job process's starter (Jobs()), and %s (@rpc, @schedule(%q)), which starts %s through it", rpcDir, w.method, s.schedule, w.command)

		return rpcDir, true, nil
	}
	ch.didf("%s: %s (@rpc, @schedule(%q)), which starts %s through the Client's Jobs()", methodFile, w.method, s.schedule, w.command)
	// An application's own Client carries the starter when it declares Jobs(), as one
	// an earlier job wired does; one without is given the starter by the agent.
	methods, err := methodsOfType(a, rpcDir, rpcClientType)
	if err != nil {
		return "", false, err
	}
	if !slices.Contains(methods, jobsAccessor) {
		ch.skipf("%s: give %s a Jobs() jobs.Starter accessor fed from the configuration's Jobs() (the job driver, resource/jobs/cloudrun), which %s starts the job through", rpcDir, rpcClientType, w.method)

		return rpcDir, false, nil
	}

	return rpcDir, true, nil
}

// rpcClientSource is a fresh rpc package's Client.
func (starterWiring) rpcClientSource(pkg string) string {
	return fmt.Sprintf(`// Package %[1]s defines the application's RPC methods: an @rpc struct per method whose Execute
// signature says how it runs (resource/README.md §7), and the Client that carries the
// application's dependencies into them. The generator reads the package (WithRPC in the
// generator program) and emits each method's handler and route.
package %[1]s

import "%[2]s"

// Client carries application dependencies into RPC method implementations: the starter
// of the job process, which the scheduled methods start the job process's commands
// through.
type Client struct {
	jobs jobs.Starter
}

// NewClient constructs a Client over the job process's starter.
func NewClient(starter jobs.Starter) *Client {
	return &Client{jobs: starter}
}

// Jobs is the job process's starter (resource/jobs): the job driver the configuration
// built.
func (c *Client) Jobs() jobs.Starter {
	return c.jobs
}
`, pkg, jobsImportPath)
}

// editSiteConfig gives the site level the scheduled routes' guard and the job driver,
// the guard read from the environment and the driver opened from the settings the site's
// environment struct embeds, where the level is built, with their accessors in a new file
// beside it. What the level holds already is left as it is: an application whose own
// scheduled method wired the guard gains the driver alone, one that wired a starter of
// its own keeps it, and an accessor the package declares is not written twice.
func (w starterWiring) editSiteConfig(a *app.App, ch *Change) error {
	rel, src, mode, err := findDeclaringFile(a, siteConfigType)
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a %s struct, so the scheduled routes' guard and the job driver are not built; build them where the served site's configuration is (scheduled.FromEnvironment; the job driver's Settings embedded in the site's environment struct and its Open, the driver bound under the alias %s) and expose them as %s() and %s()", siteConfigType, jobDriverAlias, schedulerAccessor, jobsAccessor)

		return nil
	}
	pkg, err := app.PackageName(rel, src)
	if err != nil {
		return err
	}
	guardField, err := app.StructFieldOfType(rel, src, siteConfigType, scheduledImportPath, "Guard")
	if err != nil {
		return err
	}
	starterField, driver, err := w.starterField(rel, src)
	if err != nil {
		return err
	}
	wrapErr := plainErr
	if ok, _ := app.HasImport(rel, src, errorsImportPath); ok {
		wrapErr = `errors.Wrap(err, %q)`
	}
	edit := &sourceEdit{rel: rel, src: src}
	var added, wired []string
	built := true
	if guardField == "" {
		guardField = siteGuardField
		ok, err := w.buildInSite(edit, &siteWiring{
			importPath: scheduledImportPath,
			field:      "// " + siteGuardField + " is the guard the scheduled routes sit behind (" + scheduledConfigFile + ").\n" + siteGuardField + " *scheduled.Guard",
			statements: "// The scheduled routes' guard (" + scheduledConfigFile + "): it reads the invoker identity the stack\n// names in APP_SCHEDULER_INVOKER, and without one logs that the scheduled routes are off.\n" +
				siteGuardField + ", err := scheduled.FromEnvironment(ctx)\nif err != nil {\n\treturn nil, " + wrapFor(wrapErr, "scheduled.FromEnvironment()") + "\n}",
			callee:  "scheduled.FromEnvironment",
			element: siteGuardField + ": " + siteGuardField,
			what:    "the guard",
		})
		if err != nil {
			return err
		}
		built = built && ok
		added = append(added, "the "+siteGuardField+" field")
	} else {
		wired = append(wired, "the guard")
	}
	if starterField == "" {
		starterField, driver = siteStarterField, true
		ok, err := w.buildInSite(edit, &siteWiring{
			importPath: cloudrunImportPath,
			importName: jobDriverAlias,
			field:      "// " + siteStarterField + " is the job driver: the starter of this build's job, or of none (" + scheduledConfigFile + ").\n" + siteStarterField + " *" + jobDriverAlias + ".Driver",
			statements: "// The job driver (" + scheduledConfigFile + "): it names the job of this build from the template job the\n// stack sets in APP_JOBS_TEMPLATE, the setting the site's environment embeds, and the\n// version the image bakes in, and without a template refuses every start.\n" +
				"starter, err := " + jobDriverAlias + ".Open(ctx, env.Settings, data.AppVersion())\nif err != nil {\n\treturn nil, " + wrapFor(wrapErr, jobDriverAlias+".Open()") + "\n}",
			callee:  jobDriverAlias + ".Open",
			element: siteStarterField + ": starter",
			what:    "the driver",
		})
		if err != nil {
			return err
		}
		built = built && ok
		added = append(added, "the "+siteStarterField+" field")
		w.embedDriverSettings(edit)
	} else {
		wired = append(wired, "the starter")
	}
	if len(added) > 0 {
		if err := os.WriteFile(a.Abs(rel), edit.src, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
	}
	accessors, accessorsFile, err := w.writeSiteAccessors(a, rel, pkg, guardField, starterField, driver)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s: %s gained %s", rel, siteConfigType, gainedInSite(added, built, wired))
	if closed := slices.Index(accessors, closeMethod+"()"); closed >= 0 {
		accessors = slices.Delete(accessors, closed, closed+1)
		if len(accessors) > 0 {
			line += fmt.Sprintf("; %s: %s, which the app's %s asks for, and %s(), which releases the driver with the level", accessorsFile, joinAnd(accessors), configurerType, closeMethod)
		} else {
			line += fmt.Sprintf("; %s: %s(), which releases the driver with the level", accessorsFile, closeMethod)
		}
	} else if len(accessors) > 0 {
		line += fmt.Sprintf("; %s: %s, which the app's %s asks for", accessorsFile, joinAnd(accessors), configurerType)
	}
	ch.didf("%s", line)
	edit.report(ch)

	return nil
}

// starterField finds the field the site level holds its job starter in: the job driver
// (*cloudrun.Driver), reported as such, or a starter of the application's own
// (jobs.Starter); empty when the level holds neither.
func (starterWiring) starterField(rel string, src []byte) (field string, driver bool, err error) {
	field, err = app.StructFieldOfType(rel, src, siteConfigType, cloudrunImportPath, "Driver")
	if err != nil || field != "" {
		return field, field != "", err
	}
	field, err = app.StructFieldOfValueType(rel, src, siteConfigType, jobsImportPath, "Starter")

	return field, false, err
}

// embedDriverSettings embeds the job driver's settings in the site's environment struct,
// so the application declares the template job's variable through the driver's
// declaration; a site level without an environment struct leaves the embedding to the
// agent.
func (starterWiring) embedDriverSettings(e *sourceEdit) {
	m := envStructRE.FindSubmatch(e.src)
	if m == nil {
		e.skipped = append(e.skipped, fmt.Sprintf("%s: no environment struct (env := &T{}) to embed %s.Settings in; embed it in the site's environment struct, which declares APP_JOBS_TEMPLATE, and pass it to %s.Open", e.rel, jobDriverAlias, jobDriverAlias))

		return
	}
	if err := e.apply(app.AddStructField(e.rel, e.src, string(m[1]), "\n// The job driver's variables ("+jobDriverAlias+".Settings, whichever driver the import names:\n// the Cloud Run driver's template job this build's job is named from).\n"+jobDriverAlias+".Settings")); err != nil {
		e.skipped = append(e.skipped, fmt.Sprintf("%s: %s.Settings was not embedded in %s (%v); embed it there, since it declares APP_JOBS_TEMPLATE", e.rel, jobDriverAlias, m[1], err))

		return
	}
	e.gained = append(e.gained, jobDriverAlias+".Settings on "+string(m[1]))
}

// writeSiteAccessors writes the site level's scheduled file with the accessors the
// package lacks, Scheduler() over the guard field and Jobs() over the starter field, and
// Close when the starter is the job driver and the type declares no Close, so the driver
// is released with the level; it returns the methods written and the file; none written,
// no file is.
func (starterWiring) writeSiteAccessors(a *app.App, rel, pkg, guardField, starterField string, driver bool) (accessors []string, file string, err error) {
	methods, err := methodsOfType(a, path.Dir(rel), siteConfigType)
	if err != nil {
		return nil, "", err
	}
	var imports []string
	var body strings.Builder
	if !slices.Contains(methods, schedulerAccessor) {
		accessors, imports = append(accessors, schedulerAccessor+"()"), append(imports, scheduledImportPath)
		fmt.Fprintf(&body, schedulerAccessorSource, schedulerAccessor, siteConfigType, guardField)
	}
	if !slices.Contains(methods, jobsAccessor) {
		accessors, imports = append(accessors, jobsAccessor+"()"), append(imports, jobsImportPath)
		fmt.Fprintf(&body, jobsAccessorSource, jobsAccessor, siteConfigType, starterField)
	}
	if driver && !slices.Contains(methods, closeMethod) {
		accessors = append(accessors, closeMethod+"()")
		fmt.Fprintf(&body, closeSiteSource, closeMethod, siteConfigType, starterField, dataConfigType)
	}
	file = path.Join(path.Dir(rel), scheduledConfigFile)
	if len(accessors) == 0 {
		return nil, file, nil
	}
	if err := writeGo(a, file, goFile(pkg, imports, body.String())); err != nil {
		return nil, "", err
	}

	return accessors, file, nil
}

// gainedInSite says what the site level took: both fields, one of them beside the one
// the application had wired, or nothing.
func gainedInSite(added []string, built bool, wired []string) string {
	from := ""
	if built {
		from = ", built from the environment"
	}
	switch {
	case len(added) == 0:
		return "nothing: " + joinAnd(wired) + " were wired already"
	case len(wired) == 0:
		return "the fields" + from
	default:
		return added[0] + from + " (" + wired[0] + " was wired already)"
	}
}

// siteWiring is one of the two values the site level gains: its field with the import
// the type needs, with the name the file binds it under (empty for the package's own),
// the statements that build it before the construction, the call those statements make
// (present already, they are not added), and the literal element that sets the field.
type siteWiring struct {
	importPath, importName, field, statements, callee, element, what string
}

// buildInSite adds one wiring to the site level: the field and its import, the
// statements unless the constructor makes the call already, and the element. It reports
// whether the value is built where the level is constructed; a constructor without the
// literal leaves the field declared and the building to the agent.
func (starterWiring) buildInSite(e *sourceEdit, w *siteWiring) (bool, error) {
	if err := e.apply(app.AddStructField(e.rel, e.src, siteConfigType, w.field)); err != nil {
		return false, err
	}
	if err := e.apply(app.AddNamedImport(e.rel, e.src, w.importName, w.importPath)); err != nil {
		return false, err
	}
	calls, err := app.HasCall(e.rel, e.src, "", siteConstructor, w.callee)
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: no %s to build %s in; call %s(ctx) where the level is built and set the field", e.rel, siteConstructor, w.what, w.callee))

		return false, nil
	case err != nil:
		return false, err
	}
	if !calls {
		built, err := app.AddStatementsBeforeConstruction(e.rel, e.src, siteConstructor, siteConfigType, w.statements)
		switch {
		case errors.Is(err, app.ErrNoAnchor):
			e.skipped = append(e.skipped, fmt.Sprintf("%s: %s builds no &%s{...} literal, so %s is declared but not built; call %s(ctx) where the level is built and set the field", e.rel, siteConstructor, siteConfigType, w.what, w.callee))

			return false, nil
		case err != nil:
			return false, err
		}
		e.src = built
	}
	if err := e.apply(app.AddLiteralElement(e.rel, e.src, siteConstructor, siteConfigType, w.element)); err != nil {
		return false, err
	}

	return true, nil
}

// wrapFor renders an error return: the wrap template with its message, or err alone.
func wrapFor(wrapErr, message string) string {
	if wrapErr == plainErr {
		return plainErr
	}

	return fmt.Sprintf(wrapErr, message)
}

// The site level's accessors, one per value: the accessor's name, the type, and the
// field it reads.
const (
	schedulerAccessorSource = `
// %[1]s returns the guard the scheduled routes sit behind: Cloud Scheduler's tokens of
// the invoker identity APP_SCHEDULER_INVOKER names, and nothing else. With the variable
// unset, as in development, every scheduled call is refused.
func (c *%[2]s) %[1]s() *scheduled.Guard {
	return c.%[3]s
}
`
	jobsAccessorSource = `
// %[1]s starts the application's job process: the job driver (resource/jobs/cloudrun),
// which names the job of this build from the template job the stack sets in
// APP_JOBS_TEMPLATE and the version the image bakes in, or refuses every start where no
// template is configured.
func (c *%[2]s) %[1]s() jobs.Starter {
	return c.%[3]s
}
`
	closeSiteSource = `
// %[1]s releases the job driver, then the levels below.
func (c *%[2]s) %[1]s() {
	c.%[3]s.%[1]s()
	c.%[4]s.%[1]s()
}
`
)

// goFile is a Go file: the package clause, the imports, the standard library's in a
// group of their own ahead of the rest, and the body.
func goFile(pkg string, imports []string, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n", pkg)
	if len(imports) > 0 {
		var std, rest []string
		for _, importPath := range imports {
			if first, _, _ := strings.Cut(importPath, "/"); strings.Contains(first, ".") {
				rest = append(rest, importPath)
			} else {
				std = append(std, importPath)
			}
		}
		b.WriteString("\nimport (\n")
		for _, importPath := range std {
			fmt.Fprintf(&b, "\t%q\n", importPath)
		}
		if len(std) > 0 && len(rest) > 0 {
			b.WriteString("\n")
		}
		for _, importPath := range rest {
			fmt.Fprintf(&b, "\t%q\n", importPath)
		}
		b.WriteString(")\n")
	}
	b.WriteString(body)

	return b.String()
}

// editApp exposes the guard and the RPC client on the App: the Configurer asks the
// configuration for the guard and the starter, the App carries the guard and the client
// built over the starter, and a new file declares the middleware the generated router
// requires (SchedulerAuth) and the accessor the generated handlers require (RPCClient).
// What the app declares already, as an application with a scheduled method of its own
// does, is left as it is.
func (w starterWiring) editApp(a *app.App, modulePath, rpcDir string, hasClient bool, ch *Change) error {
	rel, src, mode, err := findDeclaringFile(a, configurerType)
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a %s interface, so the guard and the RPC client are not exposed on the app; the generated router needs %s(next http.Handler) http.Handler (scheduled.Guard's Middleware) and the generated handlers %s() returning the rpc package's *%s, built over the configuration's %s()", configurerType, schedulerAuth, rpcClientAccessor, rpcClientType, jobsAccessor)

		return nil
	}
	pkg, err := app.PackageName(rel, src)
	if err != nil {
		return err
	}
	edit := &sourceEdit{rel: rel, src: src}
	guardField, err := w.exposeGuard(edit)
	if err != nil {
		return err
	}
	rpcPath, rpcPkg := modulePath+"/"+rpcDir, path.Base(rpcDir)
	if err := w.exposeRPCClient(edit, rpcPath, rpcPkg, rpcDir, hasClient); err != nil {
		return err
	}
	if len(edit.gained) > 0 {
		if err := os.WriteFile(a.Abs(rel), edit.src, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
	}
	methods, err := methodsOfType(a, path.Dir(rel), appType)
	if err != nil {
		return err
	}
	writeAuth := !slices.Contains(methods, schedulerAuth)
	writeClient := hasClient && !slices.Contains(methods, rpcClientAccessor)
	line := fmt.Sprintf("%s: %s gained %s", rel, appType, joinAnd(edit.gained))
	if len(edit.gained) == 0 {
		line = fmt.Sprintf("%s: %s exposed the guard and the starter already", rel, appType)
	}
	if writeAuth || writeClient {
		scheduledFile := path.Join(path.Dir(rel), scheduledAppFile)
		if err := writeGo(a, scheduledFile, w.appSource(pkg, rpcPath, rpcDir, rpcPkg, guardField, writeAuth, writeClient)); err != nil {
			return err
		}
		var declared string
		switch {
		case writeAuth && writeClient:
			declared = schedulerAuth + ", the middleware the generated router mounts the scheduled routes behind, and " + rpcClientAccessor + "(), the dependencies of the RPC methods"
		case writeAuth:
			declared = schedulerAuth + ", the middleware the generated router mounts the scheduled routes behind"
		default:
			declared = rpcClientAccessor + "(), the dependencies of the RPC methods"
		}
		line += fmt.Sprintf("; %s: %s", scheduledFile, declared)
	}
	ch.didf("%s", line)
	edit.report(ch)

	return nil
}

// exposeGuard asks the Configurer for the guard and the starter, and gives the App the
// guard. It returns the App's guard field: the one the application declared, or the one
// added here.
func (starterWiring) exposeGuard(e *sourceEdit) (string, error) {
	var onConfigurer []string
	for _, m := range []struct{ name, line, importPath string }{
		{schedulerAccessor, "// " + schedulerAccessor + " is the guard the scheduled routes sit behind (resource/scheduled): the\n// invoker identity the stack names in APP_SCHEDULER_INVOKER, whose Google-signed tokens\n// alone are admitted. A guard with no invoker refuses every scheduled call.\n" + schedulerAccessor + "() *scheduled.Guard", scheduledImportPath},
		{jobsAccessor, "// " + jobsAccessor + " starts the application's job process (resource/jobs): an execution of the\n// Cloud Run job deployed with this revision, named from the template job the stack sets in\n// APP_JOBS_TEMPLATE and the version the image bakes in.\n// The scheduled methods start the job process's commands through it.\n" + jobsAccessor + "() jobs.Starter", jobsImportPath},
	} {
		before := e.src
		if err := e.apply(app.AddInterfaceLine(e.rel, e.src, configurerType, m.line)); err != nil {
			return "", err
		}
		if bytes.Equal(before, e.src) {
			continue
		}
		if err := e.apply(app.AddImport(e.rel, e.src, m.importPath)); err != nil {
			return "", err
		}
		onConfigurer = append(onConfigurer, m.name+"()")
	}
	if len(onConfigurer) > 0 {
		e.gained = append(e.gained, joinAnd(onConfigurer)+" on "+configurerType)
	}
	field, err := app.StructFieldOfType(e.rel, e.src, appType, scheduledImportPath, "Guard")
	if err != nil {
		return "", err
	}
	if field != "" {
		return field, nil
	}
	if err := e.apply(app.AddStructField(e.rel, e.src, appType, appGuardField+" *scheduled.Guard")); err != nil {
		return "", err
	}
	if err := e.apply(app.AddImport(e.rel, e.src, scheduledImportPath)); err != nil {
		return "", err
	}
	e.gained = append(e.gained, "the "+appGuardField+" field")
	with, err := app.AddLiteralElement(e.rel, e.src, appConstructor, appType, appGuardField+": cfg."+schedulerAccessor+"()")
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: %s builds no %s literal, so the guard is not set; set %s to cfg.%s()", e.rel, appConstructor, appType, appGuardField, schedulerAccessor))
	case err != nil:
		return "", err
	default:
		e.src = with
		e.gained = append(e.gained, "its construction")
	}

	return appGuardField, nil
}

// exposeRPCClient gives the App the RPC client built over the starter when the rpc
// package is this transition's; an application's own client is left as it is, and
// feeding it the starter is the agent's.
func (w starterWiring) exposeRPCClient(e *sourceEdit, rpcPath, rpcPkg, rpcDir string, hasClient bool) error {
	if !hasClient {
		if rpcDir != "" {
			e.skipped = append(e.skipped, fmt.Sprintf("%s: the app's %s() keeps the client it builds; hand it the configuration's %s() so %s can start the job", e.rel, rpcClientAccessor, jobsAccessor, w.method))
		}

		return nil
	}
	// An App carrying the client already, as one an earlier job wired does, keeps it.
	field, err := app.StructFieldOfType(e.rel, e.src, appType, rpcPath, rpcClientType)
	if err != nil {
		return err
	}
	if field != "" {
		return nil
	}
	if err := e.apply(app.AddImport(e.rel, e.src, rpcPath)); err != nil {
		return err
	}
	if err := e.apply(app.AddStructField(e.rel, e.src, appType, "rpcClient *"+rpcPkg+"."+rpcClientType)); err != nil {
		return err
	}
	with, err := app.AddLiteralElement(e.rel, e.src, appConstructor, appType, "rpcClient: "+rpcPkg+".NewClient(cfg."+jobsAccessor+"())")
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: %s builds no %s literal, so the RPC client is not built; set rpcClient to %s.NewClient(cfg.%s())", e.rel, appConstructor, appType, rpcPkg, jobsAccessor))
	case err != nil:
		return err
	default:
		e.src = with
		e.gained = append(e.gained, "the RPC client built over the starter")
	}

	return nil
}

// appSource is the app's scheduled file: SchedulerAuth over the App's guard field, and
// RPCClient when the rpc package is this transition's, each when the app lacks it.
func (starterWiring) appSource(pkg, rpcPath, rpcDir, rpcPkg, guardField string, writeAuth, writeClient bool) string {
	var imports []string
	var body strings.Builder
	if writeAuth {
		imports = append(imports, "net/http")
		fmt.Fprintf(&body, `
// %[1]s admits Cloud Scheduler's calls to the scheduled routes (/_scheduled): a
// token Google signed for the route's URL whose verified email is the invoker identity
// named in APP_SCHEDULER_INVOKER. Any other call answers 401, its reason logged, and with
// no invoker configured every call is refused: the routes are off in development.
func (a *%[2]s) %[1]s(next http.Handler) http.Handler {
	return a.%[3]s.Middleware(next)
}
`, schedulerAuth, appType, guardField)
	}
	if writeClient {
		imports = append(imports, rpcPath)
		fmt.Fprintf(&body, `
// %[1]s returns the dependencies for RPC method implementations (%[2]s): the starter of
// the job process, which the scheduled methods start its commands through.
func (a *%[3]s) %[1]s() *%[4]s.%[5]s {
	return a.rpcClient
}
`, rpcClientAccessor, rpcDir, appType, rpcPkg, rpcClientType)
	}

	return goFile(pkg, imports, body.String())
}

// methodsOfType lists the methods the package at dir declares on the type in its
// non-test files, so an accessor or a middleware the application wrote itself is not
// written twice.
func methodsOfType(a *app.App, dir, typeName string) ([]string, error) {
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	var methods []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		rel := path.Join(dir, name)
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		names, err := app.MethodNames(rel, src, typeName)
		if err != nil {
			return nil, err
		}
		methods = append(methods, names...)
	}

	return methods, nil
}

// editHarnesses gives every test configurer the two accessors the Configurer gained, a
// nil guard, since no suite mounts a scheduled route and a missing guard refuses every
// call, and a fake starter that records what a scheduled call starts; for the file
// store it also builds the configurer's resource client over a memory store, as the
// data level builds production's over the bucket, so a file route the application
// declares later is served in the suites rather than refused by the router. A test
// configurer is a type in a test file declaring LogExporter, the Configurer method
// every harness implements.
func (w starterWiring) editHarnesses(a *app.App, ch *Change) error {
	harnesses, err := w.harnessFiles(a)
	if err != nil {
		return err
	}
	var edited []string
	for _, h := range harnesses {
		src, mode, err := readFile(a, h.file)
		if err != nil {
			return err
		}
		edit := &sourceEdit{rel: h.file, src: src}
		var added []string
		for _, m := range []struct {
			name, method string
		}{
			{schedulerAccessor, fmt.Sprintf("\n// %[1]s is none: the suites mount no scheduled route, and a missing guard refuses\n// every scheduled call.\nfunc (%[2]s) %[1]s() *scheduled.Guard {\n\treturn nil\n}\n", schedulerAccessor, h.receiver)},
			{jobsAccessor, fmt.Sprintf("\n// %[1]s is a fake starter: no suite starts the job process, and a scheduled call that\n// does records the start.\nfunc (%[2]s) %[1]s() jobs.Starter {\n\treturn jobs.NewFake()\n}\n", jobsAccessor, h.receiver)},
		} {
			if slices.Contains(h.methods, m.name) {
				continue
			}
			edit.src = append(edit.src, m.method...)
			added = append(added, m.name+"()")
		}
		if len(added) > 0 {
			for _, importPath := range []string{scheduledImportPath, jobsImportPath} {
				if err := edit.apply(app.AddImport(edit.rel, edit.src, importPath)); err != nil {
					return err
				}
			}
		}
		if w.withStore {
			stored, err := wireHarnessStore(edit, h)
			if err != nil {
				return err
			}
			if stored {
				added = append(added, "the "+harnessStoreField+" field")
			}
		}
		edit.report(ch)
		if len(added) == 0 {
			continue
		}
		if err := os.WriteFile(a.Abs(h.file), edit.src, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		edited = append(edited, fmt.Sprintf("%s (%s: %s)", h.file, h.typeName, joinAnd(added)))
	}
	if len(edited) > 0 && w.withStore {
		ch.didf("the test configurers gained a nil guard, a fake starter and a memory file store: %s", strings.Join(edited, "; "))
	} else if len(edited) > 0 {
		ch.didf("the test configurers gained a nil guard and a fake starter: %s", strings.Join(edited, "; "))
	}

	return nil
}

// harness is one test configurer: the file declaring it, its type, the receiver as its
// methods write it (c *testConfigurer), and the methods it declares in that file.
type harness struct {
	file     string
	typeName string
	receiver string
	methods  []string
}

// harnessFiles finds the test configurers: the receiver types of a LogExporter method in
// the application's test files, outside the browser workspaces.
func (starterWiring) harnessFiles(a *app.App) ([]harness, error) {
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	webDirs := map[string]bool{}
	for _, w := range a.WebApps {
		webDirs[w.Dir] = true
	}
	var found []harness
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.Wrap(walkErr, "fs.WalkDir()")
		}
		if d.IsDir() {
			if p != "." && (webDirs[p] || skippedNames[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") || strings.HasPrefix(d.Name(), "zz_gen_") {
			return nil
		}
		data, readErr := root.ReadFile(filepath.FromSlash(p))
		if readErr != nil {
			return errors.Wrap(readErr, "os.Root.ReadFile()")
		}
		// A file the tool cannot parse declares no configurer it can edit.
		if h, ok := configurerIn(p, data); ok {
			found = append(found, h)
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "fs.WalkDir()")
	}

	return found, nil
}

// configurerIn reads a test file for a type declaring LogExporter, and the methods that
// type declares there.
func configurerIn(rel string, src []byte) (harness, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return harness{}, false
	}
	byType := map[string]*harness{}
	var order []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
			continue
		}
		recv := fd.Recv.List[0]
		typeName := receiverTypeName(recv.Type)
		if typeName == "" {
			continue
		}
		h := byType[typeName]
		if h == nil {
			h = &harness{file: rel, typeName: typeName}
			byType[typeName] = h
			order = append(order, typeName)
		}
		h.methods = append(h.methods, fd.Name.Name)
		if fd.Name.Name == configurerMarker {
			h.receiver = string(src[fset.Position(recv.Pos()).Offset:fset.Position(recv.End()).Offset])
		}
	}
	for _, typeName := range order {
		if h := byType[typeName]; h.receiver != "" {
			return *h, true
		}
	}

	return harness{}, false
}

// receiverTypeName is the type a method's receiver names, through a pointer.
func receiverTypeName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}

	return ""
}

// receiverName is the receiver's name in the named method of the type.
func receiverName(rel string, src []byte, typeName, method string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return "", errors.Wrap(err, "parser.ParseFile()")
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Name.Name != method || len(fd.Recv.List) != 1 || receiverTypeName(fd.Recv.List[0].Type) != typeName {
			continue
		}
		if len(fd.Recv.List[0].Names) == 0 {
			return "", errors.Wrapf(app.ErrNoAnchor, "%s: %s of %s names no receiver", rel, method, typeName)
		}

		return fd.Recv.List[0].Names[0].Name, nil
	}

	return "", errors.Wrapf(app.ErrNoAnchor, "%s declares no method %s of %s", rel, method, typeName)
}

// editProgram declares the rpc package to the generator where the program has no WithRPC
// yet, so the regeneration emits the scheduled method's handler and route.
func (starterWiring) editProgram(a *app.App, g *app.Generator, rpcDir string, ch *Change) error {
	if rpcDir == "" || g.RPCDir() != "" {
		return nil
	}
	src, mode, err := readFile(a, g.File)
	if err != nil {
		return err
	}
	edited, err := app.InsertOptions(g.File, src, "GenerateHandlers", []string{fmt.Sprintf("generation.WithRPC(%q)", rpcDir)})
	if err != nil {
		ch.skipf("%s: WithRPC(%q) was not added (%v); declare it so the generator reads the rpc package", g.File, rpcDir, err)

		return nil
	}
	if err := os.WriteFile(a.Abs(g.File), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: WithRPC(%q), so the generator reads the rpc package", g.File, rpcDir)

	return nil
}
