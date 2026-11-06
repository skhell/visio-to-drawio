// Command visiodrawio converts Visio .vsdx/.vstx files to draw.io.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/skhell/visio-to-drawio/internal/converter"
)

const usage = `Usage: visiodrawio [flags] file.vsdx

Converts every foreground page of a Visio file to a .drawio file.
Without -o, output goes to a folder named same as the input file.
Layout: <folder>/<name>_ALL_PAGES.drawio (every page in one file),
<folder>/single-pages/PAGE_NAME.drawio and <folder>/report/PAGE_NAME.csv

Commands:
	visiodrawio file.vsdx	run on a Visio .vsdx (also .vstx)
	visiodrawio *.vsdx		run against all files in the directory
	visiodrawio *. -force	with force flag overwrite all files
  	visiodrawio mcp         run as an MCP server over stdio

Flags:
	-o DIR					Output folder (default: folder named after the input file)
	-force					Overwrite existing output files
	-page					NAME Convert only this page (name, unique name or ID)
	-list					List pages and exit
	-nofit					Do not shrink text that would overflow its box
	-splitlayers			Keep every visible Visio layer as its own draw.io layer
	-hidelayer				A,B Keep the named Visio layers as separate, hidden draw.io layers
	-bgeditable				Leave background layers unlocked
	-nobg					Do not include BackPage background layers
	-version				Print the version
`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if err := serveMCP(context.Background()); err != nil {
			log.Error(err.Error())
			os.Exit(1)
		}
		return
	}
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	outDir := flag.String("o", "", "output folder (default: folder named after the input file)")
	force := flag.Bool("force", false, "overwrite existing output files")
	page := flag.String("page", "", "convert only this page (name, unique name or ID)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	list := flag.Bool("list", false, "list the pages and exit")
	noFit := flag.Bool("nofit", false, "do not shrink text that would overflow its box")
	split := flag.Bool("splitlayers", false, "keep every visible Visio layer as its own draw.io layer (z-order across layers may differ)")
	hideLayers := flag.String("hidelayer", "", "comma separated Visio layer names to keep as separate, hidden draw.io layers")
	bgEditable := flag.Bool("bgeditable", false, "leave background layers unlocked")
	noBG := flag.Bool("nobg", false, "do not include BackPage background layers")
	// parse flags anywhere on the command line so "a.vsdx b.vsdx -o out" works
	var files []string
	args := os.Args[1:]
	for {
		flag.CommandLine.Parse(args)
		args = flag.Args()
		if len(args) == 0 {
			break
		}
		files = append(files, args[0])
		args = args[1:]
	}
	if *showVersion {
		fmt.Println(version)
		return
	}
	if len(files) == 0 {
		flag.Usage()
		return
	}
	opt := converter.Options{NoFit: *noFit, Split: *split, NoBG: *noBG, BGEditable: *bgEditable, HideLayers: splitList(*hideLayers)}
	failed := false
	for _, f := range files {
		dir := *outDir
		if dir != "" && len(files) > 1 {
			dir = filepath.Join(dir, strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
		}
		if err := run(f, dir, *force, *page, *list, opt); err != nil {
			if errors.Is(err, errLegacyVSD) {
				log.Warn("skipped: legacy binary .vsd is not supported, open it in Visio and Save As .vsdx", "file", f)
				continue
			}
			log.Error(err.Error(), "file", f)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// log writes one logfmt line per event: time=<UTC> level=<LEVEL> msg="..."
var log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
	ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			a.Value = slog.StringValue(a.Value.Time().UTC().Format("2006-01-02T15:04:05Z"))
		}
		return a
	},
}))

var errLegacyVSD = errors.New("legacy .vsd")

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]+`)

// fileStem turns a page title into an upper-case, underscore separated
// file name.
func fileStem(title string) string {
	s := strings.Trim(nonAlnum.ReplaceAllString(title, "_"), "_")
	if s == "" {
		s = "PAGE"
	}
	return strings.ToUpper(s)
}

type job struct {
	page   *converter.Page
	drawio string
	report string
}

func plan(pages []*converter.Page, dir string) []job {
	used := map[string]int{}
	jobs := make([]job, 0, len(pages))
	for _, pg := range pages {
		stem := fileStem(pg.Name())
		if n := used[stem]; n > 0 {
			stem = fmt.Sprintf("%s_%d", stem, n+1)
		}
		used[fileStem(pg.Name())]++
		jobs = append(jobs, job{
			page:   pg,
			drawio: filepath.Join(dir, "single-pages", stem+".drawio"),
			report: filepath.Join(dir, "report", stem+".csv"),
		})
	}
	return jobs
}

func run(in, outDir string, force bool, pageKey string, list bool, opt converter.Options) error {
	if strings.EqualFold(filepath.Ext(in), ".vsd") {
		return errLegacyVSD
	}
	pkg, err := converter.Open(in)
	if err != nil {
		return fmt.Errorf("%s: %w", in, err)
	}
	pages := pkg.ForegroundPages()
	if pageKey != "" {
		pg := pkg.FindPage(pageKey)
		if pg == nil {
			return fmt.Errorf("page %q not found", pageKey)
		}
		pages = []*converter.Page{pg}
	}
	if list {
		for _, pg := range pages {
			fmt.Println(pg.Name())
		}
		return nil
	}
	if len(pages) == 0 {
		return errors.New("no foreground pages found")
	}

	if outDir == "" {
		outDir = strings.TrimSuffix(filepath.Base(in), filepath.Ext(in))
	}
	jobs := plan(pages, outDir)
	allPath := filepath.Join(outDir, fileStem(filepath.Base(outDir))+"_ALL_PAGES.drawio")

	if !force {
		var exists []string
		for _, j := range jobs {
			for _, f := range []string{j.drawio, j.report, allPath} {
				if _, err := os.Stat(f); err == nil {
					exists = append(exists, f)
				}
			}
		}
		if len(exists) > 0 {
			return fmt.Errorf("%d output file(s) already exist, e.g. %s (use -force to overwrite)", len(exists), exists[0])
		}
	}
	for _, d := range []string{"report", "single-pages"} {
		if err := os.MkdirAll(filepath.Join(outDir, d), 0o755); err != nil {
			return err
		}
	}

	failed := 0
	var diagrams []string
	for i, j := range jobs {
		res, err := converter.Convert(pkg, j.page, opt)
		if err != nil {
			log.Error("page conversion failed", "page", j.page.Name(), "err", err)
			failed++
			continue
		}
		if err := os.WriteFile(j.drawio, []byte(res.XML), 0o644); err != nil {
			return err
		}
		if err := writeReport(j.report, res); err != nil {
			return err
		}
		diagrams = append(diagrams, diagramOf(res.XML, i+1))
		log.Info("wrote file", "path", j.drawio)
		for _, w := range res.WarningLines() {
			log.Warn(w, "page", j.page.Name())
		}
	}
	if len(diagrams) > 0 {
		all := `<mxfile host="` + converter.Host + `">` + strings.Join(diagrams, "") + "</mxfile>\n"
		if err := os.WriteFile(allPath, []byte(all), 0o644); err != nil {
			return err
		}
		log.Info("wrote file", "path", allPath)
	}
	if failed > 0 {
		return fmt.Errorf("%d page(s) failed", failed)
	}
	return nil
}

func writeReport(path string, res *converter.Result) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return converter.WriteReport(f, res.Report)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// diagramOf extracts the <diagram> element of a single-page mxfile and
// gives it a unique id.
func diagramOf(xml string, n int) string {
	i := strings.Index(xml, "<diagram ")
	j := strings.LastIndex(xml, "</diagram>")
	if i < 0 || j < 0 {
		return ""
	}
	d := xml[i : j+len("</diagram>")]
	return strings.Replace(d, `id="d1"`, fmt.Sprintf(`id="d%d"`, n), 1) + "\n"
}
