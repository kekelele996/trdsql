package trdsql

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewImporter(t *testing.T) {
	type args struct {
		inputFormat Format
	}
	tests := []struct {
		name string
		args args
		want Format
	}{
		{
			name: "test1",
			args: args{inputFormat: CSV},
			want: CSV,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewImporter(InFormat(CSV)); !reflect.DeepEqual(got.ReadOpts.InFormat, tt.want) {
				t.Errorf("NewImporter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestImporter_Import(t *testing.T) {
	type fields struct {
		ReadOpts *ReadOpts
	}
	tests := []struct {
		name    string
		fields  fields
		query   string
		want    string
		wantErr bool
	}{
		{
			name:    "test1",
			fields:  fields{ReadOpts: NewReadOpts()},
			query:   "SELECT 1",
			want:    "SELECT 1",
			wantErr: false,
		},
		{
			name:    "testNoFile",
			fields:  fields{ReadOpts: NewReadOpts()},
			query:   "SELECT * FROM ttttnofile.csv",
			want:    "SELECT * FROM ttttnofile.csv",
			wantErr: false,
		},
		{
			name:    "testGenerate_series",
			fields:  fields{ReadOpts: NewReadOpts()},
			query:   "SELECT * FROM generate_series(1,10)",
			want:    "SELECT * FROM generate_series(1,10)",
			wantErr: false,
		},
		{
			name:    "testGenerate_series2",
			fields:  fields{ReadOpts: NewReadOpts()},
			query:   "SELECT * FROM generate_series(1,10,2)",
			want:    "SELECT * FROM generate_series(1,10,2)",
			wantErr: false,
		},
		{
			name:    "test3",
			fields:  fields{ReadOpts: NewReadOpts(InDelimiter("ddd"))},
			query:   "SELECT * FROM testdata/test.csv",
			want:    "SELECT * FROM testdata/test.csv",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := Connect(DefaultDriver, "")
			if err != nil {
				t.Fatal(err)
			}
			db.Tx, err = db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			i := &ReadFormat{
				ReadOpts: tt.fields.ReadOpts,
			}
			got, err := i.Import(db, tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("Importer.Import() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Importer.Import() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_listTable(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantTbl map[string]string
		wantIdx []int
	}{
		{
			name:    "noTable",
			query:   "SELECT 1;",
			wantTbl: map[string]string{},
			wantIdx: []int{},
		},
		{
			name:    "testTable",
			query:   "SELECT * FROM test;",
			wantTbl: map[string]string{"test": "test"},
			wantIdx: []int{6},
		},
		{
			name:    "testJoin",
			query:   "SELECT test.a FROM test LEFT JOIN test2 ON (test.b = test2.b);",
			wantTbl: map[string]string{"test": "test", "test2": "test2"},
			wantIdx: []int{6, 12},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTbl, gotIdx := TableNames(SQLFields(tt.query))
			if !reflect.DeepEqual(gotTbl, tt.wantTbl) {
				t.Errorf("TableNames() Table Map = %v, want %v", gotTbl, tt.wantTbl)
			}
			if !reflect.DeepEqual(gotIdx, tt.wantIdx) {
				t.Errorf("TableNames() Table Index = %v, want %v", gotIdx, tt.wantIdx)
			}
		})
	}
}

func Test_SQLFields(t *testing.T) {
	type args struct {
		query string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "testNo",
			args: args{query: ""},
			want: []string{},
		},
		{
			name: "testDoubleQuote",
			args: args{query: `SELECT * FROM "C:\file with a space.csv"`},
			want: []string{`SELECT`, ` `, `*`, ` `, `FROM`, ` `, `"C:\file with a space.csv"`},
		},
		{
			name: "testSingleQuote",
			args: args{query: `SELECT * FROM 'C:\file with a space.csv'`},
			want: []string{`SELECT`, ` `, `*`, ` `, `FROM`, ` `, `'C:\file with a space.csv'`},
		},
		{
			name: "testSingleQuote2",
			args: args{query: "SELECT * FROM jame's.csv"},
			want: []string{"SELECT", " ", "*", " ", "FROM", " ", "jame's.csv"},
		},
		{
			name: "testBackQuote",
			args: args{query: "SELECT * FROM `C:\file with a space.csv`;"},
			want: []string{`SELECT`, ` `, `*`, ` `, `FROM`, ` `, "`C:\file with a space.csv`", `;`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SQLFields(tt.args.query); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SQLFields() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func newDBTestSqlite3() *DB {
	db, err := Connect(DefaultDriver, "")
	if err != nil {
		return nil
	}
	return db
}

func newDBTestPostgres() *DB {
	db, err := Connect("postgres", pgDsn())
	if err != nil {
		return nil
	}
	if err := db.Ping(); err != nil {
		return nil
	}
	return db
}

func newDBTestMysql() *DB {
	db, err := Connect("mysql", myDsn())
	if err != nil {
		return nil
	}
	if err = db.Ping(); err != nil {
		return nil
	}
	return db
}

func csvReadOpts() *ReadOpts {
	opts := NewReadOpts()
	opts.InFormat = CSV
	return opts
}

func TestImportFile(t *testing.T) {
	type args struct {
		db       *DB
		fileName string
		opts     *ReadOpts
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "testNoFile",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "nofile",
				opts:     NewReadOpts(),
			},
			want:    "",
			wantErr: false,
		},
		{
			name: "testGlobFile",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "testdata/test*.csv",
				opts:     NewReadOpts(),
			},
			want:    "`testdata/test*.csv`",
			wantErr: false,
		},
		{
			name: "testNoMatch",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "testdata/testtttttt*.csv",
				opts:     NewReadOpts(),
			},
			want:    "",
			wantErr: false,
		},
		{
			name: "testCSV",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "testdata/test.csv",
				opts:     csvReadOpts(),
			},
			want:    "`testdata/test.csv`",
			wantErr: false,
		},
		{
			name: "testSqlite",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "testdata/test.csv",
				opts:     NewReadOpts(),
			},
			want:    "`testdata/test.csv`",
			wantErr: false,
		},
		{
			name: "testPostgres",
			args: args{
				db:       newDBTestPostgres(),
				fileName: "testdata/test.csv",
				opts:     NewReadOpts(),
			},
			want:    "\"testdata/test.csv\"",
			wantErr: false,
		},
		{
			name: "testMysql",
			args: args{
				db:       newDBTestMysql(),
				fileName: "testdata/test.csv",
				opts:     NewReadOpts(),
			},
			want:    "`testdata/test.csv`",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.args.db == nil {
				t.SkipNow()
			}
			var err error
			tt.args.db.Tx, err = tt.args.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			got, err := ImportFile(tt.args.db, tt.args.fileName, tt.args.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ImportFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ImportFile() = %v, want %v", got, tt.want)
			}
			if err = tt.args.db.Tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err = tt.args.db.Disconnect(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func Test_guessFormat(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		want      Format
	}{
		{name: "testCSV", tableName: "test.csv", want: CSV},
		{name: "testLTSV", tableName: "test.ltsv", want: LTSV},
		{name: "testTSV", tableName: "test.tsv", want: TSV},
		{name: "testPSV", tableName: "test.psv", want: PSV},
		{name: "testgz", tableName: "test.ltsv.gz", want: LTSV},
		{name: "testzstd", tableName: "test.ltsv.zst", want: LTSV},
		{name: "testlz4", tableName: "test.ltsv.lz4", want: LTSV},
		{name: "testbzip2", tableName: "test.ltsv.bz2", want: LTSV},
		{name: "testJSON", tableName: "test.json", want: JSON},
		{name: "testTBLN", tableName: "test.tbln", want: TBLN},
		{name: "testunknown", tableName: "test.go", want: CSV},
		{name: "testunknown2", tableName: "testltsv", want: CSV},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := guessFormat(tt.tableName); got != tt.want {
				t.Errorf("guessFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_trimQuote(t *testing.T) {
	type args struct {
		fileName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "test1",
			args: args{"test"},
			want: "test",
		},
		{
			name: "test2",
			args: args{"`test`"},
			want: "test",
		},
		{
			name: "test3",
			args: args{"\"test\""},
			want: "test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimQuote(tt.args.fileName); got != tt.want {
				t.Errorf("trimQuote() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_uncompressedReader(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     string
	}{
		{
			name:     "testGzFile",
			fileName: filepath.Join(dataDir, "test.csv.gz"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testBzip2File",
			fileName: filepath.Join(dataDir, "test.csv.bz2"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testZSTDFile",
			fileName: filepath.Join(dataDir, "test.csv.zst"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testLZ4File",
			fileName: filepath.Join(dataDir, "test.csv.lz4"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testXZFile",
			fileName: filepath.Join(dataDir, "test.csv.xz"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testNoGzFile",
			fileName: filepath.Join(dataDir, "testNoGzFile.gz"),
			want:     "1,Orange\n2,Melon\n3,Apple",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := os.Open(tt.fileName)
			if err != nil {
				t.Fatalf("extFileReader() file open error %s:%s", tt.fileName, err)
			}
			got := uncompressedReader(file)
			r, err := io.ReadAll(got)
			if err != nil {
				t.Fatalf("extFileReader() read error %s:%s", tt.fileName, err)
			}
			if !reflect.DeepEqual(string(r)[0:7], tt.want[0:7]) {
				t.Errorf("extFileReader() = %v, want %v", string(r)[0:7], tt.want[0:7])
			}
		})
	}
}

func writeTestFile(t *testing.T, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(name, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// corruptGzip has a valid gzip magic number and fixed header,
// but a garbage (non-deflate) body.
var corruptGzip = []byte{
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff,
	'c', 'o', 'r', 'r', 'u', 'p', 't', '!', '!', '!',
}

func TestGlobFileOpen_StableOrder(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.csv"), []byte("1,one"))
	writeTestFile(t, filepath.Join(dir, "b.csv"), []byte("2,two"))
	writeTestFile(t, filepath.Join(dir, "c.csv"), []byte("3,three"))

	r, err := globFileOpen(filepath.Join(dir, "*.csv"))
	if err != nil {
		t.Fatalf("globFileOpen() error = %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	want := "1,one\n2,two\n3,three\n"
	if string(got) != want {
		t.Errorf("globFileOpen() = %q, want %q", got, want)
	}
}

func TestGlobFileOpen_UnreadablePreflight(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can open files regardless of permission bits")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "good1.csv"), []byte("1,one\n"))
	noPerm := filepath.Join(dir, "noperm.csv")
	writeTestFile(t, noPerm, []byte("2,two\n"))
	if err := os.Chmod(noPerm, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(noPerm, 0o644) })

	_, err := globFileOpen(filepath.Join(dir, "*.csv"))
	if err == nil {
		t.Fatal("expected error for unreadable file, got nil")
	}
	if !errors.Is(err, ErrBatchInput) {
		t.Errorf("error = %v, want errors.Is ErrBatchInput", err)
	}
	if !strings.Contains(err.Error(), "noperm.csv") {
		t.Errorf("error %q does not name the offending file", err)
	}
}

func TestGlobFileOpen_DirectoryMatched(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "good.csv"), []byte("1,one\n"))
	if err := os.Mkdir(filepath.Join(dir, "subdir.csv"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := globFileOpen(filepath.Join(dir, "*.csv"))
	if err == nil {
		t.Fatal("expected error for matched directory, got nil")
	}
	if !errors.Is(err, ErrBatchInput) {
		t.Errorf("error = %v, want errors.Is ErrBatchInput", err)
	}
	if !strings.Contains(err.Error(), "subdir.csv") {
		t.Errorf("error %q does not name the offending entry", err)
	}
}

func TestGlobFileOpen_CorruptCompressed(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "good1.csv"), []byte("1,one\n"))
	writeTestFile(t, filepath.Join(dir, "good2.csv"), []byte("2,two\n"))
	writeTestFile(t, filepath.Join(dir, "zcorrupt.csv.gz"), corruptGzip)

	r, err := globFileOpen(filepath.Join(dir, "*.csv*"))
	if err != nil {
		t.Fatalf("preflight should pass, got %v", err)
	}
	_, rerr := io.ReadAll(r)
	if rerr == nil {
		t.Fatal("expected read error for corrupt gzip, got nil")
	}
	if !errors.Is(rerr, ErrBatchInput) {
		t.Errorf("error = %v, want errors.Is ErrBatchInput", rerr)
	}
	if !strings.Contains(rerr.Error(), "zcorrupt.csv.gz") {
		t.Errorf("error %q does not name the corrupt file", rerr)
	}
}

func TestExec_BatchFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permission bits")
	}
	tests := []struct {
		name       string
		setup      func(t *testing.T, dir string)
		reportFile string
	}{
		{
			name: "permissionDenied",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "good1.csv"), []byte("1,one\n"))
				writeTestFile(t, filepath.Join(dir, "good2.csv"), []byte("2,two\n"))
				noPerm := filepath.Join(dir, "noperm.csv")
				writeTestFile(t, noPerm, []byte("3,three\n"))
				if err := os.Chmod(noPerm, 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(noPerm, 0o644) })
			},
			reportFile: "noperm.csv",
		},
		{
			name: "corruptGzip",
			setup: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "good1.csv"), []byte("1,one\n"))
				writeTestFile(t, filepath.Join(dir, "good2.csv"), []byte("2,two\n"))
				writeTestFile(t, filepath.Join(dir, "zcorrupt.csv.gz"), corruptGzip)
			},
			reportFile: "zcorrupt.csv.gz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)

			outStream := new(bytes.Buffer)
			trd := setDefaultTRDSQL(outStream)
			glob := filepath.Join(dir, "*.csv*")
			err := trd.Exec("SELECT * FROM " + glob)
			if err == nil {
				t.Fatal("expected Exec error, got nil")
			}
			if !errors.Is(err, ErrBatchInput) {
				t.Errorf("Exec error = %v, want errors.Is ErrBatchInput", err)
			}
			if !strings.Contains(err.Error(), tt.reportFile) {
				t.Errorf("error %q does not name %s", err, tt.reportFile)
			}
			if outStream.Len() != 0 {
				t.Errorf("partial results were output: %q", outStream.String())
			}
		})
	}
}

// TestGlobImport_RollbackRemovesTable verifies that when a batch fails
// after the temporary table has been created and rows inserted,
// rolling back the transaction leaves no table behind.
func TestGlobImport_RollbackRemovesTable(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "good1.csv"), []byte("1,one"))
	writeTestFile(t, filepath.Join(dir, "good2.csv"), []byte("2,two"))
	writeTestFile(t, filepath.Join(dir, "zcorrupt.csv.gz"), corruptGzip)

	dbFile := filepath.Join(t.TempDir(), "trdsql.db")
	db, err := Connect(DefaultDriver, dbFile)
	if err != nil {
		t.Fatal(err)
	}
	// Keep every transaction on the same connection so that
	// sqlite_temp_master state is observable below.
	db.SetMaxOpenConns(1)

	glob := filepath.Join(dir, "*.csv*")

	db.Tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter()
	_, err = importer.ImportContext(context.Background(), db, "SELECT * FROM "+glob)
	if err == nil {
		t.Fatal("expected import error, got nil")
	}
	if !errors.Is(err, ErrBatchInput) {
		t.Errorf("import error = %v, want errors.Is ErrBatchInput", err)
	}
	if !strings.Contains(err.Error(), "zcorrupt.csv.gz") {
		t.Errorf("error %q does not name the corrupt file", err)
	}
	if err := db.Tx.Rollback(); err != nil {
		t.Fatalf("rollback: %s", err)
	}

	db.Tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Select("SELECT name FROM sqlite_temp_master WHERE type='table'")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Errorf("temporary tables left behind after rollback: %v", names)
	}
	if err := db.Disconnect(); err != nil {
		t.Fatal(err)
	}
}
