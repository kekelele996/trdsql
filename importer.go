package trdsql

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/ulikunitz/xz"
)

var (
	// ErrInvalidColumn is returned if invalid column.
	ErrInvalidColumn = errors.New("invalid column")
	// ErrNoReader is returned when there is no reader.
	ErrNoReader = errors.New("no reader")
	// ErrUnknownFormat is returned if the format is unknown.
	ErrUnknownFormat = errors.New("unknown format")
	// ErrNoRows returned when there are no rows.
	ErrNoRows = errors.New("no rows")
	// ErrUnableConvert is returned if it cannot be converted to a table.
	ErrUnableConvert = errors.New("unable to convert")
	// ErrNoMatchFound is returned if no match is found.
	ErrNoMatchFound = errors.New("no match found")
	// ErrBatchInput is returned if a file in a wildcard (batch)
	// input cannot be opened or read.
	ErrBatchInput = errors.New("batch input error")
	// ErrNonDefinition is returned when there is no definition.
	ErrNonDefinition = errors.New("no definition")
	// ErrInvalidJSON is returned when the JSON is invalid.
	ErrInvalidJSON = errors.New("invalid JSON")
	// ErrInvalidYAML is returned when the YAML is invalid.
	ErrInvalidYAML = errors.New("invalid YAML")
)

// Importer is the interface for importing data into the database.
// Importer parses SQL queries to decide which files to import.
// Therefore, the reader does not receive it directly.
type Importer interface {
	Import(db *DB, query string) (string, error)
	ImportContext(ctx context.Context, db *DB, query string) (string, error)
}

// ReadFormat implements the Importer interface.
type ReadFormat struct {
	*ReadOpts
}

// NewImporter returns the default Importer.
// The arguments are functional options.
//
// Usage:
//
//	trdsql.NewImporter(
//		trdsql.InFormat(trdsql.CSV),
//		trdsql.InHeader(true),
//		trdsql.InDelimiter(";"),
//	)
func NewImporter(options ...ReadOpt) *ReadFormat {
	readOpts := NewReadOpts(options...)
	return &ReadFormat{
		ReadOpts: readOpts,
	}
}

// DefaultDBType is the default column type.
const DefaultDBType = "text"

// Import parses the SQL statement and imports one or more tables.
// Import is called from Exec.
// It returns the rewritten SQL and an error.
// No error is returned if there is no table to import.
func (i *ReadFormat) Import(db *DB, query string) (string, error) {
	ctx := context.Background()
	return i.ImportContext(ctx, db, query)
}

// ImportContext parses the SQL statement and imports one or more tables.
// ImportContext is called from ExecContext.
// It returns the rewritten SQL and an error.
// No error is returned if there is no table to import.
func (i *ReadFormat) ImportContext(ctx context.Context, db *DB, query string) (string, error) {
	parsedQuery := SQLFields(query)
	tables, tableIdx := TableNames(parsedQuery)
	if len(tables) == 0 {
		// without FROM clause. ex. SELECT 1+1;
		debug.Printf("table not found\n")
		return query, nil
	}

	for fileName := range tables {
		tableName, err := ImportFileContext(ctx, db, fileName, i.ReadOpts)
		if err != nil {
			return query, err
		}
		if len(tableName) > 0 {
			tables[fileName] = tableName
		}
	}

	// replace table names in query with their quoted values
	for _, idx := range tableIdx {
		if table, ok := tables[parsedQuery[idx]]; ok {
			parsedQuery[idx] = table
		}
	}

	// reconstruct the query with quoted table names
	query = strings.Join(parsedQuery, "")
	return query, nil
}

// TableNames returns a map of table names
// that may be tables by a simple SQL parser
// from the query string of the argument,
// along with the locations within the parsed
// query where those table names were found.
func TableNames(parsedQuery []string) (map[string]string, []int) {
	tables := make(map[string]string)
	tableIdx := []int{}
	tableFlag := false
	frontFlag := false
	debug.Printf("[%s]", strings.Join(parsedQuery, "]["))
	for i, w := range parsedQuery {
		switch {
		case strings.Contains(" \t\r\n;=", w): //nolint // Because each character is parsed by SQLFields.
			continue
		case strings.EqualFold(w, "FROM"),
			strings.EqualFold(w, "*FROM"),
			strings.EqualFold(w, "JOIN"),
			strings.EqualFold(w, "TABLE"),
			strings.EqualFold(w, "INTO"),
			strings.EqualFold(w, "UPDATE"):
			tableFlag = true
			frontFlag = true
		case isSQLKeyWords(w):
			tableFlag = false
		case w == ",":
			frontFlag = true
		default:
			if tableFlag && frontFlag {
				if w[len(w)-1] == ')' {
					w = w[:len(w)-1]
				}
				if !isSQLKeyWords(w) {
					tables[w] = w
					tableIdx = append(tableIdx, i)
				}
			}
			frontFlag = false
		}
	}
	return tables, tableIdx
}

// SQLFields returns an array of string fields
// (interpreting quotes) from the argument query.
func SQLFields(query string) []string {
	parsed := make([]string, 0, len(query)/2)
	buf := new(bytes.Buffer)
	var singleQuoted, doubleQuoted, backQuote bool
	for _, r := range query {
		switch r {
		case ' ', '\t', '\r', '\n', ',', ';', '=', '(', ')':
			if !singleQuoted && !doubleQuoted && !backQuote {
				if buf.Len() != 0 {
					parsed = append(parsed, buf.String())
					buf.Reset()
				}
				parsed = append(parsed, string(r))
			} else {
				buf.WriteRune(r)
			}
			continue
		case '\'':
			if !doubleQuoted && !backQuote {
				singleQuoted = !singleQuoted
			}
		case '"':
			if !singleQuoted && !backQuote {
				doubleQuoted = !doubleQuoted
			}
		case '`':
			if !singleQuoted && !doubleQuoted {
				backQuote = !backQuote
			}
		case '*':
			str := buf.String()
			if strings.ToUpper(str) == "SELECT" { // `SELECT*` to `SELECT *`
				parsed = append(parsed, str)
				parsed = append(parsed, string(r))
				buf.Reset()
				continue
			}
		}
		buf.WriteRune(r)
	}
	if buf.Len() > 0 {
		parsed = append(parsed, buf.String())
	}
	return parsed
}

func isSQLKeyWords(str string) bool {
	switch strings.ToUpper(str) {
	case "WHERE", "GROUP", "HAVING", "WINDOW", "UNION", "ORDER", "LIMIT", "OFFSET", "FETCH",
		"FOR", "LEFT", "RIGHT", "CROSS", "INNER", "FULL", "LATERAL", "(SELECT":
		return true
	}
	return false
}

// ImportFile imports a file.
// It returns the quoted table name and an error.
// If a single file is not found, it does not import and returns no error.
// Wildcards can be passed as fileName: matched files are concatenated
// in lexical order, but an error is returned if any matched file
// cannot be opened or read.
func ImportFile(db *DB, fileName string, readOpts *ReadOpts) (string, error) {
	return ImportFileContext(context.Background(), db, fileName, readOpts)
}

// ImportFileContext imports a file with context.
// It returns the quoted table name and an error.
// If a single file is not found, it does not import and returns no error.
// Wildcards can be passed as fileName: matched files are concatenated
// in lexical order, but an error is returned if any matched file
// cannot be opened or read.
func ImportFileContext(ctx context.Context, db *DB, fileName string, readOpts *ReadOpts) (string, error) {
	opts, fileName := GuessOpts(readOpts, fileName)
	db.importCount++
	file, err := importFileOpen(fileName)
	if err != nil {
		if errors.Is(err, ErrBatchInput) {
			// A wildcard matched files but one of them cannot be read:
			// fail explicitly instead of producing partial results.
			return "", err
		}
		// A single file that cannot be opened (e.g. not found) is
		// ignored so it may be a table inside the database.
		// A wildcard matching nothing behaves the same way.
		debug.Printf("%s\n", err)
		return "", nil
	}

	defer func() {
		if deferr := file.Close(); deferr != nil {
			log.Printf("file close:%s", deferr)
		}
	}()

	reader, err := NewReader(file, opts)
	if err != nil {
		return "", err
	}

	tableName := fileName
	if opts.InJQuery != "" {
		tableName = fmt.Sprintf("%s::jq%d", fileName, db.importCount)
	}
	tableName = db.QuotedName(tableName)

	if opts.InRowNumber {
		reader = newRowNumberReader(reader)
	}

	columnNames, err := reader.Names()
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return tableName, err
		}
		debug.Printf("EOF reached before argument number of rows")
	}
	columnTypes, err := reader.Types()
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return tableName, err
		}
		debug.Printf("EOF reached before argument number of rows")
	}
	debug.Printf("Column Names: [%v]", strings.Join(columnNames, ","))
	debug.Printf("Column Types: [%v]", strings.Join(columnTypes, ","))

	if err := db.CreateTableContext(ctx, tableName, columnNames, columnTypes, opts.IsTemporary); err != nil {
		return tableName, err
	}

	return tableName, db.ImportContext(ctx, tableName, columnNames, reader)
}

// GuessOpts guesses ReadOpts from the file name and sets it.
func GuessOpts(readOpts *ReadOpts, fileName string) (*ReadOpts, string) {
	if _, err := os.Stat(fileName); err != nil {
		if idx := strings.Index(fileName, "::"); idx != -1 {
			// jq expression.
			readOpts.InJQuery = fileName[idx+2:]
			fileName = fileName[:idx]
		}
	}

	if readOpts.InFormat != GUESS {
		readOpts.realFormat = readOpts.InFormat
		return readOpts, fileName
	}

	format := guessFormat(fileName)
	readOpts.realFormat = format
	debug.Printf("Guess file type as %s: [%s]", readOpts.realFormat, fileName)
	return readOpts, fileName
}

// guessFormat guesses the format from the file name extension.
// Format extensions are searched recursively to remove
// compression extensions such as .gz.
func guessFormat(fileName string) Format {
	fileName = strings.TrimRight(fileName, "\"'`")
	for {
		dotExt := filepath.Ext(fileName)
		if dotExt == "" {
			debug.Printf("Set in CSV because the extension is unknown: [%s]", fileName)
			return CSV
		}
		ext := strings.ToUpper(strings.TrimLeft(dotExt, "."))
		if format, ok := extToFormat[ext]; ok {
			return format
		}
		fileName = fileName[:len(fileName)-len(dotExt)]
	}
}

// importFileOpen opens the file specified as a table.
func importFileOpen(tableName string) (io.ReadCloser, error) {
	r := regexp.MustCompile(`\*|\?|\[`)
	if r.MatchString(tableName) {
		return globFileOpen(tableName)
	}
	return singleFileOpen(tableName)
}

// uncompressedReader returns the decompressed reader
// if it is a compressed file.
// If a compressed stream is detected but cannot be decompressed,
// it is treated as a normal stream (legacy behavior for single files).
func uncompressedReader(reader io.Reader) io.ReadCloser {
	r, _, err := compressedReader(reader)
	if err != nil {
		// r falls back to the raw stream.
		return r
	}
	return r
}

// compressedReader detects compression by the magic number
// and returns a decompressing reader.
// It returns matched=false if the stream is not compressed.
// It returns an error if a compression magic number is detected
// but the decompressor cannot be initialized (corrupt header).
// On error the returned reader is the raw stream for fallback use.
func compressedReader(reader io.Reader) (r io.ReadCloser, matched bool, err error) {
	buf := [7]byte{}
	n, rerr := io.ReadAtLeast(reader, buf[:], len(buf))
	if rerr != nil {
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			return io.NopCloser(bytes.NewReader(buf[:n])), false, nil
		}
		return io.NopCloser(bytes.NewReader(nil)), false, nil
	}

	rd := io.MultiReader(bytes.NewReader(buf[:n]), reader)
	switch {
	case bytes.Equal(buf[:3], []byte{0x1f, 0x8b, 0x8}):
		r, err = gzip.NewReader(rd)
	case bytes.Equal(buf[:3], []byte{0x42, 0x5A, 0x68}):
		r = io.NopCloser(bzip2.NewReader(rd))
	case bytes.Equal(buf[:4], []byte{0x28, 0xb5, 0x2f, 0xfd}):
		var zr *zstd.Decoder
		zr, err = zstd.NewReader(rd)
		r = io.NopCloser(zr)
	case bytes.Equal(buf[:4], []byte{0x04, 0x22, 0x4d, 0x18}):
		r = io.NopCloser(lz4.NewReader(rd))
	case bytes.Equal(buf[:7], []byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x0, 0x0}):
		var zr *xz.Reader
		zr, err = xz.NewReader(rd)
		r = io.NopCloser(zr)
	default:
		return io.NopCloser(rd), false, nil
	}

	if err != nil {
		return io.NopCloser(rd), true, err
	}
	return r, true, nil
}

// singleFileOpen opens one file. Also interpret stdin.
func singleFileOpen(fileName string) (io.ReadCloser, error) {
	if len(fileName) == 0 || fileName == "-" || strings.ToLower(fileName) == "stdin" {
		return uncompressedReader(bufio.NewReader(os.Stdin)), nil
	}
	fileName = expandTilde(trimQuote(fileName))
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	return uncompressedReader(file), nil
}

// globFileOpen expands the file path,
// connects multiple files and returns one io.PipeReader.
// Matched file names are concatenated in lexical order.
// If any matched file is not a regular file or cannot be opened,
// an error wrapping ErrBatchInput is returned before streaming starts.
// If a file cannot be read (including corrupt compressed content)
// while streaming, the error is propagated to the reader,
// naming the offending file, and the remaining files are not read.
func globFileOpen(globName string) (*io.PipeReader, error) {
	globName = expandTilde(trimQuote(globName))
	fileNames, err := filepath.Glob(globName)
	if err != nil {
		return nil, err
	}
	if len(fileNames) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoMatchFound, globName)
	}

	// Preflight: batch import fails if any matched file cannot be read.
	for _, fileName := range fileNames {
		if err := checkFileReadable(fileName); err != nil {
			return nil, fmt.Errorf("%w: %s: %s", ErrBatchInput, fileName, err)
		}
	}

	pipeReader, pipeWriter := io.Pipe()
	go func() {
		for _, fileName := range fileNames {
			if err := copyFileOpen(pipeWriter, fileName); err != nil {
				// Stop the whole batch and report the offending file.
				_ = pipeWriter.CloseWithError(
					fmt.Errorf("%w: %s: %s", ErrBatchInput, fileName, err))
				return
			}
		}
		if err := pipeWriter.Close(); err != nil {
			log.Printf("pipe close:%s", err)
		}
	}()
	return pipeReader, nil
}

// checkFileReadable verifies that the matched file is a regular file
// and can be opened for reading before the batch import starts.
func checkFileReadable(fileName string) error {
	info, err := os.Stat(fileName)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file (mode %s)", info.Mode())
	}
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	return file.Close()
}

// copyFileOpen opens the file and copies it to the writer.
// A corrupt compressed stream returns an error instead of
// silently falling back to the raw content.
func copyFileOpen(writer io.Writer, fileName string) error {
	debug.Printf("Open: [%s]", fileName)
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			log.Printf("file close:%s", cerr)
		}
	}()

	// Strict mode: a detected but unreadable compressed stream is an error.
	r, _, err := compressedReader(file)
	if err != nil {
		return err
	}

	if _, err := io.Copy(writer, r); err != nil {
		return err
	}
	// For if the file does not have a line break before EOF.
	if _, err := writer.Write([]byte("\n")); err != nil {
		return err
	}
	debug.Printf("Close: [%s]", fileName)
	return nil
}

func expandTilde(fileName string) string {
	if strings.HasPrefix(fileName, "~") {
		usr, err := user.Current()
		if err != nil {
			log.Printf("ERROR: %s", err)
			return fileName
		}
		fileName = filepath.Join(usr.HomeDir, fileName[1:])
	}
	return fileName
}

func trimQuote(str string) string {
	if str[0] == '`' && str[len(str)-1] == '`' {
		str = str[1 : len(str)-1]
	}
	if str[0] == '"' && str[len(str)-1] == '"' {
		str = str[1 : len(str)-1]
	}
	return str
}

func trimQuoteAll(str string) string {
	if len(str) < 2 {
		return str
	}
	if str[0] == '\'' && str[len(str)-1] == '\'' {
		return str[1 : len(str)-1]
	}
	if str[0] == '`' && str[len(str)-1] == '`' {
		return str[1 : len(str)-1]
	}
	if str[0] == '"' && str[len(str)-1] == '"' {
		return str[1 : len(str)-1]
	}
	return str
}
