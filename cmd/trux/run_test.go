package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunFileParsesValidSourceWithNoOutput(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func main() int {
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}

	if out.String() != "" {
		t.Fatalf("output = %q, want empty output", out.String())
	}
}

func TestRunFileCompilesAndExecutesIntegerProgram(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func add(a int, b int) int {
    return a + b
}

func main() int {
    let x int = add(1, 2)
    print(x)
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}

	if out.String() != "3\n" {
		t.Fatalf("output = %q, want 3\\n", out.String())
	}
}

func TestRunFileCompilesAndExecutesPrimitiveProgram(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func label() string {
    return "trux"
}

func ready() bool {
    return true
}

func main() int {
    let name string = label()
    let ok bool = ready()
    print(name, " ", 1)
    print(ok)
    print("line\nquote: \"")
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}

	want := "trux 1\ntrue\nline\nquote: \"\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestRunFileKeepsBranchAssignedLoopStringAliveForCaller(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func pick(flag bool) string {
    let acc string = ""
    let i int = 0
    for ; i < 4; i = i + 1 {
        if flag {
            acc = acc + "x"
        } else {
            acc = "ok"
        }
    }
    return acc
}

func main() int {
    let value string = pick(true)
    let junk string = "01230123012301230123012301230123" + "01230123012301230123012301230123"
    print(value)
    print(len(value))
    print(len(junk))
    let other string = pick(false)
    let junkOther string = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" + "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
    print(other)
    print(len(junkOther))
    return 0
}`)

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	want := "xxxx\n4\n64\nok\n64\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestRunFileKeepsJumpAssignedLoopStringAliveForCaller(t *testing.T) {
	requireCC(t)

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "continue",
			src: `package main
func cont(flag bool) string {
    let acc string = ""
    let i int = 0
    for ; i < 4; i = i + 1 {
        if flag {
            acc = acc + "x"
            continue
            acc = "ok"
        }
        acc = "no"
    }
    return acc
}

func main() int {
    let value string = cont(true)
    let junk string = "01230123012301230123012301230123" + "01230123012301230123012301230123"
    print(value)
    print(len(value))
    print(len(junk))
    return 0
}`,
			want: "xxxx\n4\n64\n",
		},
		{
			name: "break",
			src: `package main
func broken(flag bool) string {
    let acc string = ""
    let i int = 0
    for ; i < 4; i = i + 1 {
        if flag {
            acc = acc + "x"
            break
            acc = "ok"
        }
        acc = "no"
    }
    return acc
}

func main() int {
    let value string = broken(true)
    let junk string = "0" + "123"
    print(value)
    print(len(value))
    print(len(junk))
    return 0
}`,
			want: "x\n1\n4\n",
		},
		{
			name: "break skips post",
			src: `package main
func broken(flag bool) string {
    let acc string = ""
    let i int = 0
    for ; i < 4; acc = "ok" {
        if flag {
            acc = acc + "x"
            break
        }
        i = i + 1
    }
    return acc
}

func main() int {
    let value string = broken(true)
    let junk string = "0" + "123"
    print(value)
    print(len(value))
    print(len(junk))
    return 0
}`,
			want: "x\n1\n4\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempSource(t, tt.src)
			var out bytes.Buffer
			if err := runFile(&out, path); err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.want {
				t.Fatalf("output = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestRunFileKeepsJSONStringBytesAfterLoopAndCall(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
import "json"

func putAuthor(doc *json.MutDoc, obj *json.MutVal, name string) int {
    let owned string = "user-" + name
    json.addString(doc, obj, "author", owned)
    return 0
}

func row(n int) string {
    if n == 0 {
        return "row-" + "zero"
    }
    if n == 1 {
        return "row-" + "one"
    }
    return "row-" + "two"
}

func main() int {
    let doc *json.MutDoc = json.newDoc()
    let root *json.MutVal = json.newObject(doc)
    json.setRoot(doc, root)
    let arr *json.MutVal = json.newArray(doc)
    json.addVal(doc, root, "posts", arr)
    let i int = 0
    for i < 3 {
        let body string = row(i)
        let post *json.MutVal = json.newObject(doc)
        json.addString(doc, post, "body", body)
        putAuthor(doc, post, body)
        json.appendVal(arr, post)
        let junk string = "zzzzzzzzzzzzzzzzzzzz" + "yyyyyyyyyyyyyyyyyyyy"
        if len(junk) < 0 {
            return 1
        }
        i = i + 1
    }
    let tail string = "after-" + "write"
    print(json.write(doc))
    print(tail)
    return 0
}`)

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	want := `{"posts":[{"body":"row-zero","author":"user-row-zero"},{"body":"row-one","author":"user-row-one"},{"body":"row-two","author":"user-row-two"}]}` + "\nafter-write\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestRunFileKeepsNestedMultiReturnStringAlive(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func inner(n int) (int, string) {
    return n, "golden" + "_ember_1"
}

func mid(n int) (int, string, bool) {
    let id int, name string = inner(n)
    return id, name, true
}

func main() int {
    let id int, name string, ok bool = mid(7)
    let junk string = "bbbbbbbbbbbbbb" + "bbbbbbbbbbbbbb"
    if len(junk) < 0 {
        return 1
    }
    if !ok {
        print("not ok")
        return 1
    }
    print(id)
    print(name)
    return 0
}`)

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "7\ngolden_ember_1\n" {
		t.Fatalf("output = %q, want 7\\ngolden_ember_1\\n", out.String())
	}
}

func opensslHS256Token(t *testing.T, secret string, header string, payload string) string {
	t.Helper()
	b64 := func(raw string) string {
		return strings.TrimRight(base64.RawURLEncoding.EncodeToString([]byte(raw)), "=")
	}
	signing := b64(header) + "." + b64(payload)
	cmd := exec.Command("openssl", "dgst", "-sha256", "-hmac", secret, "-binary")
	cmd.Stdin = strings.NewReader(signing)
	sig, err := cmd.Output()
	if err != nil {
		t.Fatalf("openssl hmac: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestRunFileVerifiesOpenSSLShortHS256Secret(t *testing.T) {
	requireCC(t)
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Fatal(err)
	}

	secret := "twelve-dollar-challenge"
	good := opensslHS256Token(t, secret, `{"alg":"HS256","typ":"JWT"}`, `{"sub":"1","username":"golden_ember_1"}`)
	bad := good[:len(good)-1] + "A"
	if bad[len(bad)-1] == good[len(good)-1] {
		bad = good[:len(good)-1] + "B"
	}

	path := writeTempSource(t, `package main
import "errors"
import "jwt"

func check(token string, secret string) string {
    let claims *jwt.Claims, err error = jwt.verify(token, secret)
    if err != nil {
        return "reject " + errors.message(err)
    }
    return "accept " + jwt.getString(claims, "username")
}

func readUser(token string, secret string) (int, string, bool) {
    let claims *jwt.Claims, err error = jwt.verify(token, secret)
    if err != nil {
        return 0, "", false
    }
    let username string = jwt.getString(claims, "username")
    return 1, username, true
}

func main() int {
    print(check("`+good+`", "twelve-dollar-challenge"))
    print(check("`+bad+`", "twelve-dollar-challenge"))
    let id int, name string, ok bool = readUser("`+good+`", "twelve-dollar-challenge")
    let junk string = "bbbbbbbbbbbbbb" + "bbbbbbbbbbbbbb"
    if !ok || id != 1 || len(junk) < 0 {
        print("read-failed")
        return 1
    }
    print(name)
    return 0
}`)

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "accept golden_ember_1\n") {
		t.Fatalf("output = %q, want the genuine token accepted", got)
	}
	if !strings.Contains(got, "\nreject ") {
		t.Fatalf("output = %q, want the changed signature rejected", got)
	}
	if strings.Contains(got, "accept golden_ember_1\naccept ") {
		t.Fatalf("bad signature was accepted: %q", got)
	}
	if !strings.HasSuffix(got, "golden_ember_1\n") {
		t.Fatalf("output = %q, want the username returned across the call", got)
	}
}

func TestRunFileReturnedNewBoxSurvivesLaterAllocation(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main

type Box struct {
    n int
    label string
}

func makeBox() (*Box, error) {
    let b *Box = new(Box)
    b.n = 7
    b.label = "hello" + "-box"
    return b, nil
}

func wrap() (*Box, error) {
    let b *Box, err error = makeBox()
    return b, err
}

func main() int {
    let b *Box, err error = wrap()
    let junk *Box = new(Box)
    junk.n = 99
    junk.label = "over" + "write"
    if err != nil {
        print("err")
        return 1
    }
    print(b.n)
    print(b.label)
    return 0
}`)

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "7\nhello-box\n" {
		t.Fatalf("output = %q, want 7\\nhello-box\\n", out.String())
	}
}

func TestRunFileReturnedSQLiteHandleIsNotCloned(t *testing.T) {
	requireCC(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	src := `package main
import "sqlite"

func openDB(path string) (*sqlite.DB, error) {
    let db *sqlite.DB, err error = sqlite.open(path)
    return db, err
}

func readTwice(path string) int {
    let db *sqlite.DB, err error = openDB(path)
    if err != nil {
        return -1
    }
    let i int = 0
    let total int = 0
    for i < 2 {
        let stmt *sqlite.Stmt, perr error = sqlite.prepare(db, "SELECT 7")
        if perr != nil {
            return -2
        }
        let has bool, serr error = sqlite.step(stmt)
        if serr != nil || !has {
            return -3
        }
        total = total + sqlite.columnInt(stmt, 0)
        let ferr error = sqlite.finalize(stmt)
        if ferr != nil {
            return -4
        }
        i = i + 1
    }
    return total
}

func main() int {
    print(readTwice("` + dbPath + `"))
    print(readTwice("` + dbPath + `"))
    return 0
}`
	path := writeTempSource(t, src)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.CSource, "trux_sqlite_db_clone(trux_result_arena") || strings.Contains(result.CSource, "trux_sqlite_db_clone(&trux_frame") {
		t.Fatal("returning an unpacked sqlite.DB pointer cloned the connection")
	}

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "14\n14\n" {
		t.Fatalf("output = %q, want 14\\n14\\n", out.String())
	}
}

func TestRunFileReturnedSingleValueBoxSurvivesLaterAllocation(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main

type Box struct {
    n int
    label string
}

func makeBox() *Box {
    let b *Box = new(Box)
    b.n = 7
    b.label = "hello" + "-box"
    return b
}

func wrap() *Box {
    let b *Box = makeBox()
    return b
}

func main() int {
    let b *Box = wrap()
    let junk *Box = new(Box)
    junk.n = 99
    junk.label = "over" + "write"
    print(b.n)
    print(b.label)
    return 0
}`)

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "7\nhello-box\n" {
		t.Fatalf("output = %q, want 7\\nhello-box\\n", out.String())
	}
}

func TestRunFileReturnedSingleValueSQLiteHandleIsNotCloned(t *testing.T) {
	requireCC(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	src := `package main
import "sqlite"

func openRaw(path string) *sqlite.DB {
    let db *sqlite.DB, err error = sqlite.open(path)
    if err != nil {
        return nil
    }
    return db
}

func openDB(path string) *sqlite.DB {
    let db *sqlite.DB = openRaw(path)
    return db
}

func prep(db *sqlite.DB) *sqlite.Stmt {
    let stmt *sqlite.Stmt, err error = sqlite.prepare(db, "SELECT 7")
    if err != nil {
        return nil
    }
    return stmt
}

func prepWrap(db *sqlite.DB) *sqlite.Stmt {
    let stmt *sqlite.Stmt = prep(db)
    return stmt
}

func readTwice(path string) int {
    let db *sqlite.DB = openDB(path)
    if db == nil {
        return -1
    }
    let i int = 0
    let total int = 0
    for i < 2 {
        let stmt *sqlite.Stmt = prepWrap(db)
        if stmt == nil {
            return -2
        }
        let has bool, serr error = sqlite.step(stmt)
        if serr != nil || !has {
            return -3
        }
        total = total + sqlite.columnInt(stmt, 0)
        let ferr error = sqlite.finalize(stmt)
        if ferr != nil {
            return -4
        }
        i = i + 1
    }
    return total
}

func main() int {
    print(readTwice("` + dbPath + `"))
    print(readTwice("` + dbPath + `"))
    return 0
}`
	path := writeTempSource(t, src)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.CSource, "trux_sqlite_db_clone(trux_result_arena") || strings.Contains(result.CSource, "trux_sqlite_db_clone(&trux_frame") || strings.Contains(result.CSource, "trux_sqlite_stmt_clone(trux_result_arena") || strings.Contains(result.CSource, "trux_sqlite_stmt_clone(&trux_frame") {
		t.Fatal("returning a single-value sqlite handle cloned the connection or statement")
	}

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "14\n14\n" {
		t.Fatalf("output = %q, want 14\\n14\\n", out.String())
	}
}

func TestRunFileKeepsTempStringReturnAliveForCaller(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func bang(name string) string {
    let s string = name + "!"
    return s
}

func main() int {
    print(bang("a"))
    print(bang("b"))
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}

	if out.String() != "a!\nb!\n" {
		t.Fatalf("output = %q, want a!\\nb!\\n", out.String())
	}
}

func TestRunFileCompilesAndExecutesControlAndStringExamples(t *testing.T) {
	requireCC(t)

	tests := []struct {
		path string
		want string
	}{
		{
			path: "../../examples/control_flow.tx",
			want: "big\n0\n1\n2\ndone\n",
		},
		{
			path: "../../examples/else_if.tx",
			want: "grade B\n",
		},
		{
			path: "../../examples/primitives.tx",
			want: "trux 3 false\n2\nline\nquote: \"\n",
		},
		{
			path: "../../examples/fibonacci.tx",
			want: "iterative\n0\n1\n1\n2\n3\n5\n8\n13\n21\n34\nrecursive\n0\n1\n1\n2\n3\n5\n8\n13\n21\n34\n",
		},
		{
			path: "../../examples/loops.tx",
			want: "16 10\n16 10\n",
		},
		{
			path: "../../examples/return_paths.tx",
			want: "42\n7\n4\n0\n",
		},
		{
			path: "../../examples/strings.tx",
			want: "trux compiler\nhello, trux compiler\ntrux compiler!\nempty\ntrue\ntrue\ntrue\ntrue\nfalse\ntrue\ntrue\ntrue\n",
		},
		{
			path: "../../examples/assertions.tx",
			want: "mean 9\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var out bytes.Buffer
			err := runFile(&out, tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.want {
				t.Fatalf("output = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestRunFileCompilesAndExecutesCollectionExamples(t *testing.T) {
	requireCC(t)

	tests := []struct {
		path string
		want string
	}{
		{
			path: "../../examples/collections.tx",
			want: "3\n1 3\n9 2\n8 8 2\n5 0 7\nr ru 4\n2 2\n3 2 3\n8 2\nbolts 6\n2 6 true 0 false 2 2\n",
		},
		{
			path: "../../examples/ownership_clone.tx",
			want: "99 2\n20 30\n1 42\n7 9\nab cd 2\nrun-1 50 10\n",
		},
		{
			path: "../../examples/mut_parameters.tx",
			want: "2 2 2\n1 hello trux\n8 8\n9 9\n1 2\n1 2 2\n10 20 0\n",
		},
		{
			path: "../../examples/matrix_multiply_2x2.tx",
			want: "19 22\n43 50\n19 22\n43 50\n",
		},
		{
			path: "../../examples/jagged_lists.tx",
			want: "3 3 1\n12 12 6\n",
		},
		{
			path: "../../examples/nested_slices.tx",
			want: "1 2 3\n2.5 9.5\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var out bytes.Buffer
			err := runFile(&out, tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.want {
				t.Fatalf("output = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestRunFileCompilesAndExecutesModuleExamples(t *testing.T) {
	requireCC(t)

	tests := []struct {
		path string
		want string
	}{
		{
			path: "../../examples/modules/basic/main.tx",
			want: "7 7\n",
		},
		{
			path: "../../examples/modules/transitive/main.tx",
			want: "hello modules 42\n",
		},
		{
			path: "../../examples/modules/private_names/main.tx",
			want: "11 42\n",
		},
		{
			path: "../../examples/modules/same_package_math/main.tx",
			want: "7 25 12\n",
		},
		{
			path: "../../examples/modules/mut_parameters/main.tx",
			want: "2 20\n4 4\n7 7\n1 2\n1 2 2\n10 20 0\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var out bytes.Buffer
			err := runFile(&out, tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.want {
				t.Fatalf("output = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestBuildCommandCompilesAndRunsHTTPServer(t *testing.T) {
	requireCC(t)

	port := freeTCPPort(t)
	path := writeTempSource(t, fmt.Sprintf(`package main
import "http"

func handle(request int) int {
    let query string = http.query(request)
    let agent string = http.header(request, "User-Agent")
    let body string = http.body(request)
    http.text(request, 200, query + "|" + agent + "|" + body)
    return 0
}

func main() int {
    http.post("/hello", handle)
    http.listen(%d)
    return 0
}`, port))
	outputPath := filepath.Join(t.TempDir(), "server")
	if err := buildFile(path, outputPath); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(outputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	client := &stdhttp.Client{Timeout: 500 * time.Millisecond}
	url := fmt.Sprintf("http://127.0.0.1:%d/hello?name=trux", port)
	var resp *stdhttp.Response
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req, reqErr := stdhttp.NewRequest("POST", url, strings.NewReader("payload"))
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		req.Header.Set("User-Agent", "trux-test")
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("HTTP server did not respond: %v\nstderr:\n%s", err, stderr.String())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200, body = %q", resp.StatusCode, string(body))
	}
	if string(body) != "name=trux|trux-test|payload" {
		t.Fatalf("body = %q, want request fields", string(body))
	}

	rawHTTPStatus := func(t *testing.T, request string) string {
		t.Helper()
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 500*time.Millisecond)
		if err != nil {
			t.Fatalf("dial: %v, stderr: %s", err, stderr.String())
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write([]byte(request)); err != nil {
			t.Fatalf("write: %v, stderr: %s", err, stderr.String())
		}
		response, err := io.ReadAll(conn)
		if err != nil {
			t.Fatal(err)
		}
		line, _, _ := strings.Cut(string(response), "\r\n")
		return line
	}

	duplicateCL := "POST /hello HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Length: 5\r\nContent-Length: 7\r\n\r\npayload"
	if status := rawHTTPStatus(t, duplicateCL); status != "HTTP/1.1 400 Bad Request" {
		t.Fatalf("duplicate Content-Length status = %q, want 400", status)
	}

	transferEncoding := "POST /hello HTTP/1.1\r\nHost: 127.0.0.1\r\nTransfer-Encoding: gzip, chunked\r\nContent-Length: 7\r\n\r\npayload"
	if status := rawHTTPStatus(t, transferEncoding); status != "HTTP/1.1 400 Bad Request" {
		t.Fatalf("Transfer-Encoding status = %q, want 400", status)
	}
}

func TestHTTPKeepAliveLargeBodyAndDirectRings(t *testing.T) {
	requireCC(t)

	port := freeTCPPort(t)
	path := writeTempSource(t, fmt.Sprintf(`package main
import "http"

func handle(request int) int {
    http.text(request, 200, http.body(request))
    return 0
}

func main() int {
    http.post("/echo", handle)
    http.listen(%d)
    return 0
}`, port))
	outputPath := filepath.Join(t.TempDir(), "server")
	if err := buildFile(path, outputPath); err != nil {
		t.Fatal(err)
	}

	start := func(t *testing.T, extra []string) *exec.Cmd {
		t.Helper()
		cmd := exec.Command(outputPath)
		cmd.Env = append(os.Environ(), extra...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
		})
		deadline := time.Now().Add(3 * time.Second)
		ping := []byte("POST /echo HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Length: 4\r\nConnection: keep-alive\r\n\r\nping")
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
			if err != nil {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			_ = conn.SetDeadline(time.Now().Add(300 * time.Millisecond))
			_, _ = conn.Write(ping)
			buf := make([]byte, 512)
			n, readErr := conn.Read(buf)
			_ = conn.Close()
			if readErr == nil && bytes.Contains(buf[:n], []byte("200")) && bytes.Contains(buf[:n], []byte("ping")) {
				return cmd
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("server did not answer: %s", stderr.String())
		return nil
	}

	request := func(body string) string {
		return fmt.Sprintf("POST /echo HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Length: %d\r\nConnection: keep-alive\r\n\r\n%s", len(body), body)
	}
	readResponse := func(t *testing.T, conn net.Conn, pending *[]byte) (int, string) {
		t.Helper()
		tmp := make([]byte, 32*1024)
		for {
			if bytes.Contains(*pending, []byte("\r\n\r\n")) {
				head, rest, _ := bytes.Cut(*pending, []byte("\r\n\r\n"))
				status, _, _ := strings.Cut(string(head), "\r\n")
				length := -1
				for _, line := range strings.Split(string(head), "\r\n") {
					if strings.HasPrefix(strings.ToLower(line), "content-length:") {
						length, _ = strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
					}
				}
				if length >= 0 && len(rest) >= length {
					code := 0
					fmt.Sscanf(status, "HTTP/1.1 %d", &code)
					body := string(rest[:length])
					*pending = append([]byte(nil), rest[length:]...)
					return code, body
				}
			}
			n, err := conn.Read(tmp)
			if n > 0 {
				*pending = append(*pending, tmp[:n]...)
			}
			if err != nil {
				t.Fatalf("read response: %v\n%s", err, *pending)
			}
		}
	}

	t.Run("direct", func(t *testing.T) {
		_ = start(t, []string{"TRUX_HTTP_REQUIRE_DIRECT=1", "TRUX_HTTP_DIRECT_SLOTS=8"})
		conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		large := strings.Repeat("a", 200*1024)
		if _, err := conn.Write([]byte(request(large) + request("ok"))); err != nil {
			t.Fatal(err)
		}
		var pending []byte
		code, body := readResponse(t, conn, &pending)
		if code != 200 || body != large {
			t.Fatalf("large response = %d %d bytes", code, len(body))
		}
		code, body = readResponse(t, conn, &pending)
		if code != 200 || body != "ok" {
			t.Fatalf("second response = %d %q", code, body)
		}

		held := make([]net.Conn, 20)
		for i := range held {
			held[i], err = net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				t.Fatalf("dial %d: %v", i, err)
			}
			defer held[i].Close()
			if _, err := held[i].Write([]byte(request("x"))); err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
		for i := range held {
			_ = held[i].SetDeadline(time.Now().Add(2 * time.Second))
			var pending []byte
			code, body := readResponse(t, held[i], &pending)
			if code != 200 || body != "x" {
				t.Fatalf("conn %d = %d %q", i, code, body)
			}
		}
	})

	t.Run("file descriptors", func(t *testing.T) {
		_ = start(t, []string{"TRUX_HTTP_NO_DIRECT=1"})
		conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := io.WriteString(conn, request("fd")); err != nil {
			t.Fatal(err)
		}
		var pending []byte
		code, body := readResponse(t, conn, &pending)
		if code != 200 || body != "fd" {
			t.Fatalf("fallback response = %d %q", code, body)
		}
	})
}

func TestBuildCommandCompilesAndRunsHTTPMultiFile(t *testing.T) {
	requireCC(t)

	port := freeTCPPort(t)
	tempDir := t.TempDir()

	exDir := filepath.Join("..", "..", "examples", "projects", "http_multi_file")
	mainContent, err := os.ReadFile(filepath.Join(exDir, "main.tx"))
	if err != nil {
		t.Fatal(err)
	}
	mainContentStr := strings.Replace(string(mainContent), "8080", strconv.Itoa(port), 1)
	if err := os.WriteFile(filepath.Join(tempDir, "main.tx"), []byte(mainContentStr), 0644); err != nil {
		t.Fatal(err)
	}

	for _, sub := range []string{"middleware", "handlers"} {
		if err := os.MkdirAll(filepath.Join(tempDir, sub), 0755); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(filepath.Join(exDir, sub))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			content, err := os.ReadFile(filepath.Join(exDir, sub, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tempDir, sub, e.Name()), content, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}

	outputPath := filepath.Join(tempDir, "server")
	if err := buildFile(filepath.Join(tempDir, "main.tx"), outputPath); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(outputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	client := &stdhttp.Client{Timeout: 500 * time.Millisecond}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	deadline := time.Now().Add(3 * time.Second)
	var resp *stdhttp.Response
	for time.Now().Before(deadline) {
		resp, err = client.Get(baseURL + "/health")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server did not start: %v\nstderr:\n%s", err, stderr.String())
	}
	defer resp.Body.Close()

	healthBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("/health status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("X-Server") != "trux-uring" {
		t.Fatalf("/health X-Server = %q, want trux-uring", resp.Header.Get("X-Server"))
	}
	if resp.Header.Get("X-Health") != "healthy" {
		t.Fatalf("/health X-Health = %q, want healthy", resp.Header.Get("X-Health"))
	}
	if string(healthBody) != "{\"status\":\"ok\"}\n" {
		t.Fatalf("/health body = %q", string(healthBody))
	}

	unauthResp, err := client.Get(baseURL + "/api/users/42")
	if err != nil {
		t.Fatal(err)
	}
	defer unauthResp.Body.Close()
	unauthBody, _ := io.ReadAll(unauthResp.Body)
	if unauthResp.StatusCode != 401 {
		t.Fatalf("/api/users/42 status = %d, want 401", unauthResp.StatusCode)
	}
	if unauthResp.Header.Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("/api/users/42 WWW-Authenticate = %q, want Bearer", unauthResp.Header.Get("WWW-Authenticate"))
	}
	if string(unauthBody) != "{\"error\":\"unauthorized\"}\n" {
		t.Fatalf("/api/users/42 body = %q", string(unauthBody))
	}

	req, _ := stdhttp.NewRequest("GET", baseURL+"/api/users/42", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	authResp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer authResp.Body.Close()
	authBody, _ := io.ReadAll(authResp.Body)
	if authResp.StatusCode != 200 {
		t.Fatalf("/api/users/42 with auth status = %d, want 200", authResp.StatusCode)
	}
	if string(authBody) != "{\"id\":\"42\",\"auth_user\":\"user-42\",\"role\":\"admin\"}\n" {
		t.Fatalf("/api/users/42 with auth body = %q", string(authBody))
	}

	putReq, _ := stdhttp.NewRequest("PUT", baseURL+"/api/users/99", strings.NewReader("{\"name\":\"Alice\"}"))
	putReq.Header.Set("Authorization", "Bearer secret-token")
	putResp, err := client.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	defer putResp.Body.Close()
	putBody, _ := io.ReadAll(putResp.Body)
	if putResp.StatusCode != 200 {
		t.Fatalf("PUT /api/users/99 status = %d, want 200", putResp.StatusCode)
	}
	if string(putBody) != "{\"updated_id\":\"99\",\"payload\":{\"name\":\"Alice\"}}\n" {
		t.Fatalf("PUT /api/users/99 body = %q", string(putBody))
	}

	notfoundResp, err := client.Get(baseURL + "/unknown")
	if err != nil {
		t.Fatal(err)
	}
	defer notfoundResp.Body.Close()
	notfoundBody, _ := io.ReadAll(notfoundResp.Body)
	if notfoundResp.StatusCode != 404 {
		t.Fatalf("/unknown status = %d, want 404", notfoundResp.StatusCode)
	}
	if string(notfoundBody) != "{\"error\":\"custom not found\"}\n" {
		t.Fatalf("/unknown body = %q", string(notfoundBody))
	}
}

func TestRunFileCompilesAndExecutesNestedCollections(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func buildRows() list[list[int]] {
    let rows list[list[int]] = list[list[int]]{list[int]{1, 2}, list[int]{3}}
    append(rows, list[int]{4, 5, 6})
    return rows
}

func main() int {
    let rows list[list[int]] = buildRows()
    let copied list[list[int]] = clone(rows)
    append(copied[0], 9)

    let fixed [2][2]int = [2][2]int{[2]int{1, 2}, [2]int{3, 4}}

    let slices [][]float = make([][]float, 2)
    slices[0] = make([]float, 2)
    slices[0][1] = 2.5

    print(len(rows[0]), " ", len(copied[0]), " ", copied[0][2], " ", rows[2][2])
    print(fixed[1][1], " ", slices[0][1])
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}

	want := "2 3 9 6\n4 2.5\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestRunFileCompilesAndExecutesOwnershipCopyOut(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func middle() []int {
    let xs [3]int = [3]int{1, 2, 3}
    return xs[1:2]
}

func build() list[string] {
    let xs list[string] = list[string]{}
    append(xs, "a" + "b")
    return xs
}

func mid(xs []int) []int {
    return xs[1:2]
}

func midOwned(xs []int) []int {
    return clone(xs[1:2])
}

func frameOwnedLocal() []int {
    let xs [3]int = [3]int{1, 2, 3}
    let ys []int = clone(xs[:])
    return ys
}

func main() int {
    let local []int = middle()
    print(local[0])

    let words list[string] = build()
    print(words[0], " ", len(words))

    let xs [3]int = [3]int{1, 2, 3}
    let borrowed []int = mid(xs[:])
    let owned []int = midOwned(xs[:])
    xs[1] = 9
    print(borrowed[0], " ", owned[0])

    let framed []int = frameOwnedLocal()
    print(framed[0], " ", framed[2])
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}

	want := "2\nab 1\n9 2\n1 3\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestRunFileCompilesAndExecutesModuleProgram(t *testing.T) {
	requireCC(t)

	dir := t.TempDir()
	mainPath := writeTempFile(t, dir, "main.tx", `package main
import "math.tx"
import "words.tx"

func main() int {
    print(words.label(), " ", math.add(2, 5))
    return 0
}`)
	writeTempFile(t, dir, "math.tx", `package math
func base() int {
    return 2
}

pub func add(a int, b int) int {
    return a + b + base()
}`)
	writeTempFile(t, dir, "words.tx", `package words
pub func label() string {
    return "module"
}`)

	var out bytes.Buffer
	err := runFile(&out, mainPath)
	if err != nil {
		t.Fatal(err)
	}

	if out.String() != "module 9\n" {
		t.Fatalf("output = %q, want module 9\\n", out.String())
	}
}

func TestRunFileCompilesAndExecutesInputProgram(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
import "io"

func main() int {
    let name string = io.readLine()
    let count int = io.readInt()
    let ratio float = io.readFloat()
    let ready bool = io.readBool()
    print(name, " ", count + 1, " ", ratio, " ", ready)
    return 0
}`)

	var out bytes.Buffer
	err := runFileWithOptions(&out, path, runOptions{Stdin: strings.NewReader("Ada\n41\n2.5\ntrue\n")})
	if err != nil {
		t.Fatal(err)
	}

	if out.String() != "Ada 42 2.5 true\n" {
		t.Fatalf("output = %q, want Ada 42 2.5 true\\n", out.String())
	}
}

func TestRunFileCompilesAndExecutesPerformanceExample(t *testing.T) {
	requireCC(t)

	var out bytes.Buffer
	err := runFile(&out, "../../examples/performance_integer.tx")
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output = %q, want checksum and elapsed lines", out.String())
	}
	if lines[0] != "workload checksum 496477" {
		t.Fatalf("checksum line = %q, want stable checksum", lines[0])
	}
	if !strings.HasPrefix(lines[1], "elapsed nanos ") {
		t.Fatalf("elapsed line = %q, want elapsed nanos prefix", lines[1])
	}

	elapsed, err := strconv.ParseInt(strings.TrimPrefix(lines[1], "elapsed nanos "), 10, 64)
	if err != nil {
		t.Fatalf("parse elapsed nanos: %v", err)
	}
	if elapsed <= 0 {
		t.Fatalf("elapsed nanos = %d, want positive duration", elapsed)
	}
}

func TestRunFileCompilesAndExecutesFileAndCSVIO(t *testing.T) {
	requireCC(t)

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.txt")
	copyPath := filepath.Join(dir, "copy.txt")
	csvPath := filepath.Join(dir, "input.csv")
	outCSVPath := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(inputPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(csvPath, []byte("name,score\n\"A, B\",2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path := writeTempSource(t, `package main
import "io"
import "csv"

func main() int {
    let text string, err1 error = io.readFile(`+truxString(inputPath)+`)
    assert(err1 == nil, "read input.txt")
    let err2 error = io.writeFile(`+truxString(copyPath)+`, text + "!")
    assert(err2 == nil, "write copy.txt")

    let cells list[string], err3 error = csv.read(`+truxString(csvPath)+`, 2)
    assert(err3 == nil, "read csv")
    append(cells, "Cara")
    append(cells, "3")
    let err4 error = csv.write(`+truxString(outCSVPath)+`, cells, 2)
    assert(err4 == nil, "write csv")

    print(text)
    print(len(cells), " ", cells[2])
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}

	if out.String() != "hello\n6 A, B\n" {
		t.Fatalf("output = %q, want file/csv output", out.String())
	}
	if got := readFile(t, copyPath); got != "hello!" {
		t.Fatalf("copy file = %q, want hello!", got)
	}
	if got := readFile(t, outCSVPath); got != "name,score\n\"A, B\",2\nCara,3\n" {
		t.Fatalf("written csv = %q, want appended csv", got)
	}
}

func TestRunFileCompilesAndExecutesFilesystemOperations(t *testing.T) {
	requireCC(t)

	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "test_fs_dir")
	fileA := filepath.Join(subDir, "file_a.txt")
	fileB := filepath.Join(subDir, "file_b.txt")

	path := writeTempSource(t, `package main
import "io"

func main() int {
    let dir string = `+truxString(subDir)+`
    let fileA string = `+truxString(fileA)+`
    let fileB string = `+truxString(fileB)+`

    assert(!io.exists(dir), "dir should not exist yet")
    assert(!io.isDir(dir), "dir should not be isDir")
    assert(!io.isFile(dir), "dir should not be isFile")

    let err1 error = io.createDir(dir)
    assert(err1 == nil, "createDir should succeed")
    assert(io.exists(dir), "dir should exist")
    assert(io.isDir(dir), "dir should be isDir")
    assert(!io.isFile(dir), "dir should not be isFile")

    let errDup error = io.createDir(dir)
    assert(errDup != nil, "createDir duplicate should return error")

    let errW1 error = io.writeFile(fileB, "content b")
    assert(errW1 == nil, "write fileB")
    let errW2 error = io.writeFile(fileA, "content a")
    assert(errW2 == nil, "write fileA")

    assert(io.exists(fileA), "fileA should exist")
    assert(io.isFile(fileA), "fileA should be isFile")
    assert(!io.isDir(fileA), "fileA should not be isDir")

    let entries list[string], errRead error = io.readDir(dir)
    assert(errRead == nil, "readDir should succeed")
    assert(len(entries) == 2, "readDir should have 2 entries")
    assert(entries[0] == "file_a.txt", "entries[0] should be file_a.txt")
    assert(entries[1] == "file_b.txt", "entries[1] should be file_b.txt")

    let errRemDir error = io.remove(dir)
    assert(errRemDir != nil, "remove non-empty dir should fail")

    let errRem1 error = io.remove(fileA)
    assert(errRem1 == nil, "remove fileA")
    let errRem2 error = io.remove(fileB)
    assert(errRem2 == nil, "remove fileB")

    let errRemEmpty error = io.remove(dir)
    assert(errRemEmpty == nil, "remove empty dir")
    assert(!io.exists(dir), "dir should not exist after remove")

    print("filesystem ok")
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}

	if out.String() != "filesystem ok\n" {
		t.Fatalf("output = %q, want filesystem ok", out.String())
	}
}

func TestRunFileCompilesAndExecutesMultiReturnAndErrors(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
import "errors"
import "io"

func divmod(a int, b int) (int, int) {
    return a / b, a % b
}

func forwardDivmod(a int, b int) (int, int) {
    return divmod(a, b)
}

func makeMessage(id int) (string, error) {
    return "message " + "from id", nil
}

func forwardMessage(id int) (string, error) {
    return makeMessage(id)
}

func parsePositive(n int) (int, error) {
    if n < 0 {
        return 0, errors.new("negative number")
    }
    return n, nil
}

func main() int {
    let q int, r int = divmod(14, 3)
    print(q, " ", r)

    let _, rem int = divmod(14, 3)
    print("rem=", rem)

    let quot int, _ int = divmod(14, 3)
    print("quot=", quot)

    q, r = forwardDivmod(20, 6)
    print("forward: ", q, " ", r)

    let msg string, msgErr error = forwardMessage(42)
    assert(msgErr == nil, "forwardMessage error")
    print(msg)

    _, r = divmod(25, 4)
    print("r=", r)

    let val int, err error = parsePositive(10)
    if err != nil {
        print("error: ", errors.message(err))
    } else {
        print("ok ", val)
    }

    val, err = parsePositive(-5)
    if err != nil {
        print("error: ", errors.message(err))
        print(err)
    }

    let nilErr error = nil
    print(nilErr)
    if nilErr == nil {
        print("nilErr is nil")
    }

    let _, ioErr error = io.readFile("non_existent_file_path_12345.xyz")
    if ioErr != nil {
        print("caught io error: ", errors.message(ioErr))
    }

    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}

	want := "4 2\n" +
		"rem=2\n" +
		"quot=4\n" +
		"forward: 3 2\n" +
		"message from id\n" +
		"r=1\n" +
		"ok 10\n" +
		"error: negative number\n" +
		"negative number\n" +
		"<nil>\n" +
		"nilErr is nil\n" +
		"caught io error: failed to open file for reading\n"

	if out.String() != want {
		t.Fatalf("output =\n%q\nwant:\n%q", out.String(), want)
	}
}

func TestRunFileReturnsRuntimeBoundsError(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func main() int {
    let xs [1]int = [1]int{1}
    print(xs[1])
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty output", out.String())
	}
	if !strings.Contains(err.Error(), "trux runtime error: index 1 out of bounds for length 1") {
		t.Fatalf("error = %q, want bounds error", err.Error())
	}
}

func TestRunFileRewindsLoopTempsAndKeepsEscapes(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func temps(n int) int {
    let i int = 0
    let count int = 0
    for ; i < n; i = i + 1 {
        let temp string = "a" + "b"
        if temp == "ab" {
            count = count + 1
        }
    }
    return count
}

func kept(n int) string {
    let acc string = ""
    let i int = 0
    for ; i < n; i = i + 1 {
        acc = acc + "x"
    }
    return acc
}

func collect(n int) int {
    let words list[string] = list[string]{}
    let i int = 0
    for ; i < n; i = i + 1 {
        append(words, "a" + "b")
    }
    return len(words)
}

func word() string {
    return "a" + "b"
}

func countWords(n int) int {
    let i int = 0
    let count int = 0
    for ; i < n; i = i + 1 {
        let temp string = word()
        if temp == "ab" {
            count = count + 1
        }
    }
    return count
}

func pushWord(mut words list[string], word string) int {
    append(words, word)
    return len(words)
}

func fill(n int) int {
    let words list[string] = list[string]{}
    let i int = 0
    for ; i < n; i = i + 1 {
        pushWord(words, "a" + "b")
    }
    print(words[0], " ", words[2])
    return len(words)
}

func early() string {
    let i int = 0
    for ; i < 5; i = i + 1 {
        let temp string = "h" + "i"
        if i == 3 {
            return temp
        }
    }
    return ""
}

func leaf(n int) int {
    if n == 0 {
        return 2
    }
    let i int = 0
    let sum int = 0
    for ; i < 2; i = i + 1 {
        let inner string = "v" + "w"
        sum = sum + leaf(n - 1) + len(inner)
    }
    return sum
}

func sumViews(n int) int {
    let xs [3]int = [3]int{1, 2, 3}
    let i int = 0
    let total int = 0
    for ; i < n; i = i + 1 {
        let view []int = clone(xs[:])
        total = total + view[0] + view[2]
    }
    return total
}

func keptSlice() int {
    let xs [3]int = [3]int{4, 5, 6}
    let view []int = xs[:]
    let i int = 0
    for ; i < 3; i = i + 1 {
        view = clone(xs[:])
    }
    return view[0] + view[2]
}

func mix(n int) int {
    let nums list[int] = list[int]{}
    let i int = 0
    for ; i < n; i = i + 1 {
        let temp string = "a" + "b"
        append(nums, len(temp))
    }
    return nums[0] + nums[n - 1] + len(nums)
}

func hot(n int) int {
    let checksum int = 1
    for let i int = 0; i < n; i = i + 1 {
        checksum = checksum + i
    }
    return checksum
}

func main() int {
    print(temps(20))
    print(kept(4))
    print(collect(3))
    print(countWords(4))
    print(fill(3))
    print(early())
    print(leaf(2))
    print(sumViews(3))
    print(keptSlice())
    print(mix(4))
    print(hot(5))
    return 0
}`)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	want := "20\nxxxx\n3\n4\nab ab\n3\nhi\n20\n12\n10\n8\n11\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestRunFileReusesFrameChunksAcrossCalls(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func bang(value string) string {
    return value + "!"
}

func main() int {
    let i int = 0
    let word string = "a"
    for ; i < 5; i = i + 1 {
        word = bang(word)
    }
    print(word)
    return 0
}`)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "a!!!!!\n" {
		t.Fatalf("output = %q, want a!!!!!", out.String())
	}
}

func TestRunFileBreakAndContinue(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
import "io"

func main() int {
    let i int = 0
    let seen int = 0
    for ; i < 5; i = i + 1 {
        if i == 2 {
            break
        }
        seen = seen + 1
    }
    print(seen)
    print(i)

    let j int = 0
    let kept int = 0
    for ; j < 4; j = j + 1 {
        if j == 1 {
            continue
        }
        kept = kept + 1
    }
    print(kept)
    print(j)

    let n int = 0
    for let x int = 0; x < 3; x = x + 1 {
        for let y int = 0; y < 3; y = y + 1 {
            if y == 1 {
                break
            }
            n = n + 1
        }
    }
    print(n)

    let m int = 0
    for let x int = 0; x < 2; x = x + 1 {
        for let y int = 0; y < 3; y = y + 1 {
            if y == 1 {
                continue
            }
            m = m + 1
        }
    }
    print(m)

    let k int = 0
    for ; io.readInt() == 1; k = k + 1 {
        if k == 0 {
            continue
        }
    }
    print(k)
    return 0
}`)

	var out bytes.Buffer
	err := runFileWithOptions(&out, path, runOptions{Stdin: strings.NewReader("1\n1\n0\n")})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "2\n2\n3\n4\n3\n4\n2\n" {
		t.Fatalf("output = %q, want break and continue counts", out.String())
	}
}

func TestCompileFileEmitsBreakAndContinueInKernel(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"

kernel func fill(out gpu.Buffer[int], n int) {
    for let i int = 0; i < n; i = i + 1 {
        if i == 1 {
            continue
        }
        if i == 3 {
            break
        }
        out[0] = i
    }
}

func main() int {
    return 0
}`)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.CSource, "break;") || !strings.Contains(result.CSource, "goto trux_continue_") || !strings.Contains(result.CSource, "trux_continue_0:") {
		t.Fatalf("generated source missing loop control:\n%s", result.CSource)
	}
}

func TestRunFileShortCircuitsLogicalOperators(t *testing.T) {
	requireCC(t)

	tests := []struct {
		name  string
		src   string
		stdin string
		want  string
	}{
		{
			name: "and skips read",
			src: `package main
import "io"
func main() int {
    print(io.readInt() == 1 && io.readInt() == 2)
    return 0
}`,
			stdin: "0\n",
			want:  "false\n",
		},
		{
			name: "or skips read",
			src: `package main
import "io"
func main() int {
    print(io.readInt() == 1 || io.readInt() == 2)
    return 0
}`,
			stdin: "1\n",
			want:  "true\n",
		},
		{
			name: "and reads both",
			src: `package main
import "io"
func main() int {
    print(io.readInt() == 1 && io.readInt() == 2)
    return 0
}`,
			stdin: "1\n9\n",
			want:  "false\n",
		},
		{
			name: "or reads both",
			src: `package main
import "io"
func main() int {
    print(io.readInt() == 0 || io.readInt() == 2)
    return 0
}`,
			stdin: "5\n2\n",
			want:  "true\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempSource(t, tt.src)
			var out bytes.Buffer
			err := runFileWithOptions(&out, path, runOptions{Stdin: strings.NewReader(tt.stdin)})
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.want {
				t.Fatalf("output = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestCompileFileEmitsLogicalOperatorsInKernel(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"

kernel func fill(out gpu.Buffer[int], n int, enabled bool) {
    if enabled && n > 0 || !enabled {
        out[0] = 1
    }
}

func main() int {
    return 0
}`)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.CSource, "&&") || !strings.Contains(result.CSource, "||") {
		t.Fatalf("generated source missing logical operators:\n%s", result.CSource)
	}
}

func TestRunFileCompilesAndExecutesRemainder(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func main() int {
    print(17 % 5)
    print((10 % 3) * 2)
    return 0
}`)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "2\n2\n" {
		t.Fatalf("output = %q, want 2 and 2", out.String())
	}
}

func TestRunFileRemainderRejectsZeroAndOverflow(t *testing.T) {
	requireCC(t)

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "zero",
			src: `package main
func main() int {
    print(10 % 0)
    return 0
}`,
			want: "trux runtime error: integer remainder by zero",
		},
		{
			name: "overflow",
			src: `package main
func main() int {
    let min int = (0 - 9223372036854775807) - 1
    let minusOne int = 0 - 1
    print(min % minusOne)
    return 0
}`,
			want: "trux runtime error: integer overflow in %",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempSource(t, tt.src)
			var out bytes.Buffer
			err := runFile(&out, path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %s", err, tt.want)
			}
		})
	}
}

func TestCompileFileEmitsRemainderInKernel(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"

kernel func fill(out gpu.Buffer[int], n int) {
    out[0] = n % 3
}

func main() int {
    return 0
}`)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.CSource, "rt_rem_i64(") {
		t.Fatalf("generated source missing remainder helper:\n%s", result.CSource)
	}
}

func TestRunFileCompilesAndExecutesPrefixOperators(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func main() int {
    let n int = -4
    let f float = -1.5
    let flag bool = !false
    let grouped bool = !(1 == 2)
    print(n)
    print(f)
    print(flag)
    print(grouped)
    return 0
}`)

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "-4\n-1.5\ntrue\ntrue\n" {
		t.Fatalf("output = %q, want prefix results", out.String())
	}
}

func TestRunFileNegatingMinimumIntOverflows(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func main() int {
    let min int = (0 - 9223372036854775807) - 1
    print(-min)
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err == nil {
		t.Fatal("expected overflow")
	}
	if !strings.Contains(err.Error(), "trux runtime error: integer overflow in -") {
		t.Fatalf("error = %q, want integer overflow", err.Error())
	}
}

func TestCompileFileEmitsPrefixOperatorsForHostAndKernel(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"

kernel func scale(out gpu.Buffer[int], n int, enabled bool) {
    let i int = -gpu.globalX()
    if !enabled {
        out[0] = i
    }
}

func main() int {
    let n int = -1
    let f float = -1.5
    let flag bool = !false
    print(n)
    print(f)
    print(flag)
    return 0
}`)

	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"rt_sub_i64(0,", "!(", "-(", "__global__"} {
		if !strings.Contains(result.CSource, part) {
			t.Fatalf("generated source missing %q:\n%s", part, result.CSource)
		}
	}
}

func TestRunFileReturnsRuntimeIntegerDivisionErrors(t *testing.T) {
	requireCC(t)

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "division by zero",
			src: `package main
func main() int {
    print(10 / 0)
    return 0
}`,
			want: "trux runtime error: integer division by zero",
		},
		{
			name: "division overflow",
			src: `package main
func main() int {
    let min int = (0 - 9223372036854775807) - 1
    let minusOne int = 0 - 1
    print(min / minusOne)
    return 0
}`,
			want: "trux runtime error: integer overflow in /",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempSource(t, tt.src)

			var out bytes.Buffer
			err := runFile(&out, path)
			if err == nil {
				t.Fatal("expected error")
			}
			if out.String() != "" {
				t.Fatalf("output = %q, want empty output", out.String())
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestRunFileEvaluatesSideEffectingExpressionsLeftToRight(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
import "io"

func sub(a int, b int) int {
    return a - b
}

func main() int {
    print(sub(io.readInt(), io.readInt()))
    print(io.readInt() - io.readInt())
    return 0
}`)

	var out bytes.Buffer
	err := runFileWithOptions(&out, path, runOptions{Stdin: strings.NewReader("10\n3\n20\n8\n")})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "7\n12\n" {
		t.Fatalf("output = %q, want ordered reads", out.String())
	}
}

func TestRunFileAssertTrueContinuesWithoutEvaluatingMessage(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func message() string {
    print("message evaluated")
    return "bad"
}

func main() int {
    assert(1 == 1, message())
    print("after")
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "after\n" {
		t.Fatalf("output = %q, want after\\n", out.String())
	}
}

func TestRunFileAssertFalseCrashesApp(t *testing.T) {
	requireCC(t)

	path := writeTempSource(t, `package main
func main() int {
    print("before")
    assert(1 == 2, "numbers should match")
    print("after")
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.String() != "before\n" {
		t.Fatalf("output = %q, want before\\n", out.String())
	}
	if !strings.Contains(err.Error(), "trux runtime error: assertion failed: numbers should match") {
		t.Fatalf("error = %q, want assertion failure", err.Error())
	}
}

func TestRunFileReturnsMissingImportWithSourceContext(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeTempFile(t, dir, "main.tx", `package main
import "missing.tx"

func main() int {
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, mainPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty output", out.String())
	}

	wantParts := []string{
		mainPath + ":2:1: cannot find module \"missing.tx\"",
		"1 | package main",
		"2 | import \"missing.tx\"",
		"  | ^",
		"4 | func main() int {",
	}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestRunFileReturnsImportedTypeErrorWithImportedSourceContext(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeTempFile(t, dir, "main.tx", `package main
import "lib.tx"

func main() int {
    return lib.bad()
}`)
	libPath := writeTempFile(t, dir, "lib.tx", `package lib
pub func bad() int {
    return "wrong"
}`)

	var out bytes.Buffer
	err := runFile(&out, mainPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty output", out.String())
	}

	wantParts := []string{
		libPath + ":3:12: cannot return string from function returning int",
		"1 | package lib",
		"2 | pub func bad() int {",
		"3 |     return \"wrong\"",
		"  |            ^",
	}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestRunFileReturnsImportedParseErrorWithImportedSourceContext(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeTempFile(t, dir, "main.tx", `package main
import "lib.tx"

func main() int {
    return 0
}`)
	libPath := writeTempFile(t, dir, "lib.tx", `package main
pub func bad( int {
    return 0
}`)

	var out bytes.Buffer
	err := runFile(&out, mainPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty output", out.String())
	}

	wantParts := []string{
		libPath + ":2:15: expected IDENT, got \"int\"",
		"1 | package main",
		"2 | pub func bad( int {",
		"  |               ^",
		"3 |     return 0",
	}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestRunFileWithDebugWritesPhaseFiles(t *testing.T) {
	requireCC(t)

	t.Cleanup(func() {
		_ = os.RemoveAll("tmp")
	})

	path := writeTempSource(t, `package main
func main() int {
    let x int = 1
    print(x)
    return 0
}`)

	var out bytes.Buffer
	err := runFileWithOptions(&out, path, runOptions{Debug: true})
	if err != nil {
		t.Fatal(err)
	}

	debugDir := filepath.Join("tmp", "trux-debug", "main")
	if !strings.Contains(out.String(), "debug files: "+debugDir) {
		t.Fatalf("output = %q, want debug dir", out.String())
	}

	wantFiles := []string{
		"00-source.tx",
		"01-tokens.txt",
		"02-ast.json",
		"03-types.txt",
		"04-ir.json",
		"05-c.c",
	}
	for _, file := range wantFiles {
		path := filepath.Join(debugDir, file)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected debug file %s: %v", path, err)
		}
	}

	tokens := readFile(t, filepath.Join(debugDir, "01-tokens.txt"))
	if !strings.Contains(tokens, "IDENT\t\"print\"") {
		t.Fatalf("tokens = %q, want print token", tokens)
	}

	typeInfo := readFile(t, filepath.Join(debugDir, "03-types.txt"))
	if !strings.Contains(typeInfo, "print -> print(int)") {
		t.Fatalf("type info = %q, want resolved print call", typeInfo)
	}

	typedIR := readFile(t, filepath.Join(debugDir, "04-ir.json"))
	if !strings.Contains(typedIR, `"Type": "int"`) {
		t.Fatalf("IR = %q, want int print type", typedIR)
	}

	cSource := readFile(t, filepath.Join(debugDir, "05-c.c"))
	if !strings.Contains(cSource, "rt_print_int(trux_v_1_x);") || !strings.Contains(cSource, "rt_print_newline();") {
		t.Fatalf("generated C = %q, want int print call with newline", cSource)
	}
}

func TestRunFileWithDebugWritesPartialFilesOnTypeError(t *testing.T) {
	t.Cleanup(func() {
		_ = os.RemoveAll("tmp")
	})

	path := writeTempSource(t, `package main
func main() int {
    print()
    return 0
}`)

	var out bytes.Buffer
	err := runFileWithOptions(&out, path, runOptions{Debug: true})
	if err == nil {
		t.Fatal("expected error")
	}

	debugDir := filepath.Join("tmp", "trux-debug", "main")
	if !strings.Contains(out.String(), "debug files: "+debugDir) {
		t.Fatalf("output = %q, want debug dir", out.String())
	}
	for _, file := range []string{"00-source.tx", "01-tokens.txt", "02-ast.json"} {
		path := filepath.Join(debugDir, file)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected debug file %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(debugDir, "03-types.txt")); !os.IsNotExist(err) {
		t.Fatalf("types debug file should not exist after type error, stat err = %v", err)
	}
}

func TestRunFileReturnsLexError(t *testing.T) {
	path := writeTempSource(t, "@\n")

	var out bytes.Buffer
	err := runFile(&out, path)
	if err == nil {
		t.Fatal("expected error")
	}

	if out.String() != "" {
		t.Fatalf("output = %q, want empty output", out.String())
	}

	wantParts := []string{
		path + ":1:1: lex error: illegal character \"@\"",
		"1 | @",
		"  | ^",
	}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestRunFileReturnsSyntaxErrorWithSourceContext(t *testing.T) {
	src := `package main

func add(a int, b int) int {
    return a + b
}

func main( int {
    return 0
}`
	path := writeTempSource(t, src)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty output", out.String())
	}

	wantParts := []string{
		path + ":7:12: expected IDENT, got \"int\"",
		"5 | }",
		"6 | ",
		"7 | func main( int {",
		"  |            ^",
		"8 |     return 0",
		"9 | }",
	}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestRunFileReturnsTypeErrorWithSourceContext(t *testing.T) {
	src := `package main

func main() int {
    print()
    return 0
}`
	path := writeTempSource(t, src)

	var out bytes.Buffer
	err := runFile(&out, path)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty output", out.String())
	}

	wantParts := []string{
		path + ":4:5: print expects at least 1 argument, got 0",
		"2 | ",
		"3 | func main() int {",
		"4 |     print()",
		"  |     ^",
		"5 |     return 0",
		"6 | }",
	}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestRunCommandDoesNotPrintUsageForParseErrors(t *testing.T) {
	if !rootCmd.SilenceErrors {
		t.Fatal("root command should let execute print errors once")
	}
	if !runCmd.SilenceUsage {
		t.Fatal("run command should not print usage for source parse errors")
	}
}

func TestEmitCFileWritesGeneratedC(t *testing.T) {
	path := writeTempSource(t, `package main
func main() int {
    return 0
}`)

	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.CSource, "int main(void)") {
		t.Fatalf("generated C = %q, want C main", result.CSource)
	}
}

func TestEmitCCommandWritesSourceAndRuntimeHeader(t *testing.T) {
	path := writeTempSource(t, `package main
func main() int {
    return 0
}`)
	outDir := t.TempDir()
	var out bytes.Buffer

	oldOutDir := emitCOutDir
	t.Cleanup(func() {
		emitCOutDir = oldOutDir
		emitCCmd.SetOut(nil)
	})
	emitCOutDir = outDir
	emitCCmd.SetOut(&out)

	if err := emitCCmd.RunE(emitCCmd, []string{path}); err != nil {
		t.Fatal(err)
	}

	sourcePath := filepath.Join(outDir, filepath.Base(strings.TrimSuffix(path, filepath.Ext(path)))+".c")
	headerPath := filepath.Join(outDir, "trux_runtime.h")
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("expected generated source: %v", err)
	}
	if _, err := os.Stat(headerPath); err != nil {
		t.Fatalf("expected runtime header: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, sourcePath) || !strings.Contains(got, headerPath) {
		t.Fatalf("output = %q, want written source and header paths", got)
	}
}

func TestEmitCCommandWritesHTTPRuntimeHeader(t *testing.T) {
	path := writeTempSource(t, `package main
import "http"

func handle(request int) int {
    http.respond(request, 200, "text/plain", "ok")
    return 0
}

func main() int {
    http.get("/", handle)
    http.listen(8080)
    return 0
}`)
	outDir := t.TempDir()
	var out bytes.Buffer

	oldOutDir := emitCOutDir
	t.Cleanup(func() {
		emitCOutDir = oldOutDir
		emitCCmd.SetOut(nil)
	})
	emitCOutDir = outDir
	emitCCmd.SetOut(&out)

	if err := emitCCmd.RunE(emitCCmd, []string{path}); err != nil {
		t.Fatal(err)
	}

	httpHeaderPath := filepath.Join(outDir, "trux_http_runtime.h")
	if _, err := os.Stat(httpHeaderPath); err != nil {
		t.Fatalf("expected HTTP runtime header: %v", err)
	}
	if !strings.Contains(out.String(), httpHeaderPath) {
		t.Fatalf("output = %q, want HTTP runtime header path", out.String())
	}
}

func TestEmitCFileSupportsModuleProgram(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeTempFile(t, dir, "main.tx", `package main
import "math.tx"

func main() int {
    return math.add(1, 2)
}`)
	writeTempFile(t, dir, "math.tx", `package math
pub func add(a int, b int) int {
    return a + b
}`)

	result, err := compileFile(mainPath, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.CSource, "int64_t trux_f_16___trux_mod_1_add(") {
		t.Fatalf("generated C = %q, want module function add", result.CSource)
	}
	if !strings.Contains(result.CSource, "int main(void)") {
		t.Fatalf("generated C = %q, want C main", result.CSource)
	}
}

func TestBuildCommandRequiresOutput(t *testing.T) {
	oldOutput := buildOutput
	t.Cleanup(func() {
		buildOutput = oldOutput
	})
	buildOutput = ""

	err := buildCmd.RunE(buildCmd, []string{"main.tx"})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "build requires -o <output>" {
		t.Fatalf("error = %q, want output requirement", err.Error())
	}
}

func TestBuildFileRespectsCCEnvironmentVariable(t *testing.T) {
	path := writeTempSource(t, `package main
func main() int {
    return 0
}`)
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "cc.log")
	fakeCC := writeFakeCompiler(t, filepath.Join(dir, "fake-cc"), logPath)
	t.Setenv("CC", fakeCC)

	if err := buildFile(path, outputPath); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	if !strings.Contains(log, "-std=c11") {
		t.Fatalf("compiler args = %q, want C11 standard flag", log)
	}
	if !strings.Contains(log, "-O2") {
		t.Fatalf("compiler args = %q, want optimized build flag", log)
	}
	if !strings.Contains(log, "-o "+outputPath) {
		t.Fatalf("compiler args = %q, want output path", log)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected output executable: %v", err)
	}
}

func TestCompileGeneratedUsesO0ForDebugGeneratedC(t *testing.T) {
	path := writeTempSource(t, `package main
func main() int {
    return 0
}`)
	result, err := compileFile(path, compileOptions{Debug: true})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "cc.log")
	fakeCC := writeFakeCompiler(t, filepath.Join(dir, "fake-cc"), logPath)
	t.Setenv("CC", fakeCC)
	cPath, err := writeGeneratedFiles(dir, "main", result)
	if err != nil {
		t.Fatal(err)
	}

	if err := compileGenerated(cPath, outputPath, generatedCompileOptions{Debug: result.Debug}); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	if !strings.Contains(log, "-O0") {
		t.Fatalf("compiler args = %q, want debug build flag", log)
	}
	if strings.Contains(log, "-O2") {
		t.Fatalf("compiler args = %q, did not expect optimized debug build", log)
	}
}

func TestCompileGeneratedUsesO0ForDebugCUDA(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"

kernel func fill(out gpu.Buffer[int], n int) {
    let i int = gpu.globalX()
    if i < n {
        out[i] = 1
    }
}

func main() int {
    let out gpu.Buffer[int] = gpu.alloc(1)
    gpu.launch(fill, 1, 1, 1, 1, 1, 1, out, 1)
    gpu.free(out)
    return 0
}`)
	result, err := compileFile(path, compileOptions{Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.UsesCUDA {
		t.Fatal("expected a CUDA program")
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "nvcc.log")
	fakeNVCC := writeFakeCompiler(t, filepath.Join(dir, "fake-nvcc"), logPath)
	t.Setenv("NVCC", fakeNVCC)
	cPath, err := writeGeneratedFiles(dir, "main", result)
	if err != nil {
		t.Fatal(err)
	}

	if err := compileGenerated(cPath, outputPath, generatedCompileOptions{Debug: result.Debug, UsesCUDA: result.UsesCUDA}); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	if !strings.Contains(log, "-O0") {
		t.Fatalf("compiler args = %q, want debug build flag", log)
	}
	if strings.Contains(log, "-O2") {
		t.Fatalf("compiler args = %q, did not expect optimized debug build", log)
	}
}

func TestCompileGeneratedUsesPthreadForHTTP(t *testing.T) {
	path := writeTempSource(t, `package main
import "http"

func handle(request int) int {
    http.text(request, 200, "ok")
    return 0
}

func main() int {
    http.get("/", handle)
    http.listen(8080)
    return 0
}`)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "cc.log")
	fakeCC := writeFakeCompiler(t, filepath.Join(dir, "fake-cc"), logPath)
	t.Setenv("CC", fakeCC)
	cPath, err := writeGeneratedFiles(dir, "main", result)
	if err != nil {
		t.Fatal(err)
	}

	if err := compileGenerated(cPath, outputPath, generatedCompileOptions{UsesHTTP: result.UsesHTTP}); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	if !strings.Contains(log, "-pthread") || !strings.Contains(log, "-luring") {
		t.Fatalf("compiler args = %q, want -pthread and -luring", log)
	}
}

func TestCompileCDetectsHTTPRuntimeInclude(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.c")
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "cc.log")
	fakeCC := writeFakeCompiler(t, filepath.Join(dir, "fake-cc"), logPath)
	t.Setenv("CC", fakeCC)
	if err := os.WriteFile(sourcePath, []byte(`#include "trux_runtime.h"
#include "trux_http_runtime.h"
int main(void) { return 0; }
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := compileC(sourcePath, outputPath); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	if !strings.Contains(log, "-pthread") || !strings.Contains(log, "-luring") {
		t.Fatalf("compiler args = %q, want -pthread and -luring", log)
	}
}

func TestGeneratedExamplesCompileWithStrictCWarnings(t *testing.T) {
	requireCC(t)

	paths := exampleEntrypoints(t)
	if len(paths) == 0 {
		t.Fatal("no example programs found")
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			result, err := compileFile(path, compileOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if result.UsesCUDA {
				t.Skip("CUDA examples are compiled by nvcc, not strict C")
			}

			dir := t.TempDir()
			cPath, err := writeGeneratedFiles(dir, "main", result)
			if err != nil {
				t.Fatal(err)
			}

			compiler := os.Getenv("CC")
			if compiler == "" {
				compiler = "cc"
			}
			objPath := filepath.Join(dir, "main.o")
			cmd := exec.Command(compiler, "-std=c11", "-Wall", "-Wextra", "-pedantic", "-c", cPath, "-o", objPath)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("compile generated C: %v\n%s", err, output)
			}
			if strings.TrimSpace(string(output)) != "" {
				t.Fatalf("strict C compiler emitted warnings:\n%s", output)
			}
		})
	}
}

func TestRunFileCompilesAndExecutesGPUMatrixExample(t *testing.T) {
	requireNVCCAndCUDA(t)

	var out bytes.Buffer
	err := runFile(&out, "../../examples/gpu_matrix_multiply.tx")
	if err != nil {
		t.Fatal(err)
	}

	cpuChecksum := outputValue(t, out.String(), "cpu checksum ")
	gpuChecksum := outputValue(t, out.String(), "gpu checksum ")
	if cpuChecksum == "0" {
		t.Fatalf("cpu checksum = 0, want non-zero matrix result")
	}
	if cpuChecksum != gpuChecksum {
		t.Fatalf("checksums differ: cpu=%s gpu=%s\n%s", cpuChecksum, gpuChecksum, out.String())
	}

	wantParts := []string{
		"mismatches 0\n",
		"cpu seconds ",
		"gpu seconds ",
	}
	for _, part := range wantParts {
		if !strings.Contains(out.String(), part) {
			t.Fatalf("output = %q, want it to contain %q", out.String(), part)
		}
	}
}

func outputValue(t *testing.T, output string, prefix string) string {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("output = %q, want line with prefix %q", output, prefix)
	return ""
}

func TestBuildFileRespectsNVCCEnvironmentVariableForGPUProgram(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"

kernel func fill(out gpu.Buffer[int], n int) {
    let i int = gpu.globalX()
    if i < n {
        out[i] = 1
    }
}

func main() int {
    let out gpu.Buffer[int] = gpu.alloc(1)
    gpu.launch(fill, 1, 1, 1, 1, 1, 1, out, 1)
    gpu.free(out)
    return 0
}`)
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "nvcc.log")
	fakeNVCC := writeFakeCompiler(t, filepath.Join(dir, "fake-nvcc"), logPath)
	t.Setenv("NVCC", fakeNVCC)

	if err := buildFile(path, outputPath); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	if !strings.Contains(log, ".cu ") && !strings.HasSuffix(strings.TrimSpace(log), ".cu") {
		t.Fatalf("compiler args = %q, want CUDA source path", log)
	}
	if strings.Contains(log, "-std=c11") {
		t.Fatalf("compiler args = %q, did not expect C11 flag for nvcc", log)
	}
	if !strings.Contains(log, "-O2") {
		t.Fatalf("compiler args = %q, want optimized host build flag", log)
	}
	if !strings.Contains(log, "-o "+outputPath) {
		t.Fatalf("compiler args = %q, want output path", log)
	}
}

func TestBuildFileLinksLiburingWhenHTTPUsesGPU(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"
import "http"

kernel func fill(i int, out gpu.Buffer[int]) {
    out[i] = 1
}

func handle(request int) int {
    http.text(request, 200, "ok")
    return 0
}

func main() int {
    http.get("/", handle)
    http.listen(8080)
    return 0
}`)
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "nvcc.log")
	fakeNVCC := writeFakeCompiler(t, filepath.Join(dir, "fake-nvcc"), logPath)
	t.Setenv("NVCC", fakeNVCC)

	if err := buildFile(path, outputPath); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	for _, part := range []string{".cu ", "-D_GNU_SOURCE", "-Xcompiler", "-pthread", "-luring"} {
		if !strings.Contains(log, part) {
			t.Fatalf("compiler args = %q, want %s", log, part)
		}
	}
}

func TestBuildFileLinksYYJSONWhenGPUUsesJSON(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"
import "json"

kernel func fill(i int, out gpu.Buffer[int]) {
    out[i] = 1
}

func main() int {
    let doc *json.Doc, err error = json.parse("{\"n\":1}")
    if err != nil {
        return 1
    }
    let out gpu.Buffer[int] = gpu.alloc(1)
    gpu.launch(fill, 1, 1, 1, 1, 1, 1, 1, out)
    gpu.free(out)
    return 0
}`)
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "nvcc.log")
	fakeNVCC := writeFakeCompiler(t, filepath.Join(dir, "fake-nvcc"), logPath)
	t.Setenv("NVCC", fakeNVCC)

	if err := buildFile(path, outputPath); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	if !strings.Contains(log, "yyjson-0.11.1-") {
		t.Fatalf("compiler args = %q, want yyjson object", log)
	}
}

func TestBuildFileLinksSQLiteWhenGPUUsesSQLite(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"
import "sqlite"

kernel func fill(i int, out gpu.Buffer[int]) {
    out[i] = 1
}

func main() int {
    let db *sqlite.DB, err error = sqlite.open(":memory:")
    if err != nil {
        return 1
    }
    let _ error = sqlite.close(db)
    let out gpu.Buffer[int] = gpu.alloc(1)
    gpu.launch(fill, 1, 1, 1, 1, 1, 1, 1, out)
    gpu.free(out)
    return 0
}`)
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "nvcc.log")
	fakeNVCC := writeFakeCompiler(t, filepath.Join(dir, "fake-nvcc"), logPath)
	t.Setenv("NVCC", fakeNVCC)

	if err := buildFile(path, outputPath); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	if !strings.Contains(log, "sqlite3-3.47.2-") {
		t.Fatalf("compiler args = %q, want sqlite3 object", log)
	}
}

func TestBuildFileLinksJWTWhenGPUUsesJWT(t *testing.T) {
	path := writeTempSource(t, `package main
import "gpu"
import "jwt"

kernel func fill(i int, out gpu.Buffer[int]) {
    out[i] = 1
}

func main() int {
    let token string, err error = jwt.sign("{\"sub\":\"ada\"}", "0123456789abcdef0123456789abcdef")
    if err != nil {
        return 1
    }
    let out gpu.Buffer[int] = gpu.alloc(1)
    gpu.launch(fill, 1, 1, 1, 1, 1, 1, 1, out)
    gpu.free(out)
    if len(token) < 1 {
        return 1
    }
    return 0
}`)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.UsesCUDA || !result.UsesJWT {
		t.Fatalf("UsesCUDA=%v UsesJWT=%v, want both", result.UsesCUDA, result.UsesJWT)
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "nvcc.log")
	fakeNVCC := writeFakeCompiler(t, filepath.Join(dir, "fake-nvcc"), logPath)
	t.Setenv("NVCC", fakeNVCC)

	if err := buildFile(path, outputPath); err != nil {
		t.Fatal(err)
	}

	log := readFile(t, logPath)
	for _, part := range []string{".cu ", "-Xlinker", "-ljwt", "-ljansson", "-lcrypto", "yyjson-0.11.1-"} {
		if !strings.Contains(log, part) {
			t.Fatalf("compiler args = %q, want %s", log, part)
		}
	}
}

func TestRunFileJWTAndGPU(t *testing.T) {
	requireNVCCAndCUDA(t)
	if jwtPrefix() == "" {
		t.Skip("libjwt prefix not found")
	}

	path := writeTempSource(t, `package main
import "gpu"
import "jwt"

kernel func fill(i int, out gpu.Buffer[int]) {
    out[i] = 7
}

func main() int {
    let token string, err error = jwt.sign("{\"sub\":\"ada\"}", "0123456789abcdef0123456789abcdef")
    if err != nil {
        return 1
    }
    let parsed *jwt.Claims, verr error = jwt.verify(token, "0123456789abcdef0123456789abcdef")
    if verr != nil {
        return 1
    }
    print(jwt.getString(parsed, "sub"))
    let out gpu.Buffer[int] = gpu.alloc(1)
    gpu.launch(fill, 1, 1, 1, 1, 1, 1, 0, out)
    gpu.sync()
    let host []int = make([]int, 1)
    gpu.to(out, host)
    gpu.free(out)
    print(host[0])
    return 0
}`)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ada\n7\n" {
		t.Fatalf("got output %q, want ada and 7", out.String())
	}
}

func TestDigitsHTTPBenchmarkProgramUsesHTTPAndGPU(t *testing.T) {
	result, err := compileFile("../../benchmarks/digits/server.tx", compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.UsesCUDA || !result.UsesHTTP {
		t.Fatalf("UsesCUDA=%v UsesHTTP=%v, want both", result.UsesCUDA, result.UsesHTTP)
	}
}

func exampleEntrypoints(t *testing.T) []string {
	t.Helper()

	paths, err := filepath.Glob("../../examples/*.tx")
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.WalkDir("../../examples", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Base(path) == "main.tx" && filepath.Dir(path) != "../../examples" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return paths
}

func writeTempSource(t *testing.T, src string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "main.tx")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func writeTempFile(t *testing.T, dir string, name string, src string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

func freeTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address = %T, want *net.TCPAddr", listener.Addr())
	}
	return addr.Port
}

func requireCC(t *testing.T) {
	t.Helper()

	compiler := os.Getenv("CC")
	if compiler == "" {
		compiler = "cc"
	}
	if _, err := exec.LookPath(compiler); err != nil {
		t.Skipf("C compiler %q not found", compiler)
	}
}

func requireNVCCAndCUDA(t *testing.T) {
	t.Helper()

	compiler := os.Getenv("NVCC")
	if compiler == "" {
		compiler = "nvcc"
	}
	if _, err := exec.LookPath(compiler); err != nil {
		t.Skipf("CUDA compiler %q not found", compiler)
	}
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		t.Skip("nvidia-smi not found")
	}
	cmd := exec.Command("nvidia-smi")
	if err := cmd.Run(); err != nil {
		t.Skipf("CUDA device unavailable: %v", err)
	}
}

func writeFakeCompiler(t *testing.T, path string, logPath string) string {
	t.Helper()

	script := fmt.Sprintf(`#!/bin/sh
echo "$@" > %q
while [ "$#" -gt 0 ]; do
    if [ "$1" = "-o" ]; then
        shift
        touch "$1"
        exit 0
    fi
    shift
done
exit 2
`, logPath)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func truxString(value string) string {
	return strconv.Quote(value)
}

func TestSQLiteWithoutImportDoesNotCompileSQLite(t *testing.T) {
	src := `package main
func main() int {
    print("no sqlite")
    return 0
}
`
	path := writeTempSource(t, src)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.UsesSQLite {
		t.Fatalf("expected UsesSQLite = false, got true")
	}
	if strings.Contains(result.CSource, "trux_sqlite_runtime.h") {
		t.Fatalf("expected CSource to not include trux_sqlite_runtime.h")
	}
	for _, h := range result.RuntimeHeaders {
		if h.Name == "trux_sqlite_runtime.h" || h.Name == "sqlite3.h" {
			t.Fatalf("unexpected sqlite runtime header %s in non-sqlite program", h.Name)
		}
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "cc.log")
	fakeCC := writeFakeCompiler(t, filepath.Join(dir, "fake-cc"), logPath)
	t.Setenv("CC", fakeCC)
	cPath, err := writeGeneratedFiles(dir, "main", result)
	if err != nil {
		t.Fatal(err)
	}
	if err := compileGenerated(cPath, outputPath, generatedCompileOptions{UsesSQLite: result.UsesSQLite}); err != nil {
		t.Fatal(err)
	}
	log := readFile(t, logPath)
	if strings.Contains(log, "sqlite") {
		t.Fatalf("expected compiler args to have zero references to sqlite, got: %s", log)
	}
}

func TestSQLiteMemoryDatabase(t *testing.T) {
	src := `package main
import "sqlite"

func main() int {
    let db *sqlite.DB, err error = sqlite.open(":memory:")
    if err != nil {
        print("open err")
        return 1
    }
    let err2 error = sqlite.exec(db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INT);")
    if err2 != nil {
        print("create err")
        return 1
    }
    let err3 error = sqlite.exec(db, "INSERT INTO users (name, age) VALUES ('Alice', 30);")
    if err3 != nil {
        print("insert err")
        return 1
    }
    print(sqlite.lastInsertRowId(db))
    print(sqlite.changes(db))
    print(sqlite.totalChanges(db))

    let rows list[map[string]string], qerr error = sqlite.query(db, "SELECT id, name, age FROM users;")
    if qerr != nil {
        print("query err")
        return 1
    }
    print(len(rows))
    let first map[string]string = rows[0]
    print(first["name"])
    print(first["age"])

    let errClose error = sqlite.close(db)
    if errClose != nil {
        print("close err")
        return 1
    }
    print("closed ok")
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "1\n1\n1\n1\nAlice\n30\nclosed ok\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestSQLitePreparedStatement(t *testing.T) {
	src := `package main
import "sqlite"

func main() int {
    let db *sqlite.DB, err error = sqlite.open(":memory:")
    if err != nil {
        return 1
    }
    let _ error = sqlite.exec(db, "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, price FLOAT);")
    let stmt *sqlite.Stmt, perr error = sqlite.prepare(db, "INSERT INTO items (name, price) VALUES (?, ?);")
    if perr != nil {
        return 1
    }
    let _ error = sqlite.bindText(stmt, 1, "Gadget")
    let _ error = sqlite.bindFloat(stmt, 2, 19.99)
    let stepOk bool, serr error = sqlite.step(stmt)
    if serr != nil || stepOk {
        return 1
    }
    let _ error = sqlite.finalize(stmt)

    let qstmt *sqlite.Stmt, qerr error = sqlite.prepare(db, "SELECT id, name, price FROM items WHERE id = ?;")
    if qerr != nil {
        return 1
    }
    let _ error = sqlite.bindInt(qstmt, 1, 1)
    let hasRow bool, rerr error = sqlite.step(qstmt)
    if rerr != nil || !hasRow {
        return 1
    }
    print(sqlite.columnCount(qstmt))
    print(sqlite.columnName(qstmt, 1))
    print(sqlite.columnIndex(qstmt, "price"))
    print(sqlite.columnType(qstmt, 1))
    print(sqlite.columnInt(qstmt, 0))
    print(sqlite.columnText(qstmt, 1))
    print(sqlite.columnFloat(qstmt, 2))
    print(sqlite.columnIsNull(qstmt, 1))

    let _ error = sqlite.finalize(qstmt)
    let _ error = sqlite.close(db)
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "3\nname\n2\nTEXT\n1\nGadget\n19.99\nfalse\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestSQLiteTransactions(t *testing.T) {
	src := `package main
import "sqlite"

func main() int {
    let db *sqlite.DB, err error = sqlite.open(":memory:")
    if err != nil {
        return 1
    }
    let _ error = sqlite.exec(db, "CREATE TABLE t (v INT);")

    // Rollback test
    let _ error = sqlite.begin(db)
    let _ error = sqlite.exec(db, "INSERT INTO t VALUES (42);")
    let _ error = sqlite.rollback(db)
    let rows1 list[map[string]string], _ error = sqlite.query(db, "SELECT v FROM t;")
    print(len(rows1))

    // Commit test
    let _ error = sqlite.begin(db)
    let _ error = sqlite.exec(db, "INSERT INTO t VALUES (99);")
    let _ error = sqlite.commit(db)
    let rows2 list[map[string]string], _ error = sqlite.query(db, "SELECT v FROM t;")
    print(len(rows2))
    let r map[string]string = rows2[0]
    print(r["v"])

    let _ error = sqlite.close(db)
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "0\n1\n99\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestSQLiteFileDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	dbPathEscaped := strings.ReplaceAll(dbPath, `\`, `/`)

	src := fmt.Sprintf(`package main
import "sqlite"

func main() int {
    let db *sqlite.DB, err error = sqlite.open(%q)
    if err != nil {
        return 1
    }
    let _ error = sqlite.exec(db, "CREATE TABLE notes (id INTEGER PRIMARY KEY, content TEXT);")
    let _ error = sqlite.exec(db, "INSERT INTO notes (content) VALUES ('hello file');")
    let _ error = sqlite.close(db)

    let db2 *sqlite.DB, err2 error = sqlite.open(%q)
    if err2 != nil {
        return 1
    }
    let rows list[map[string]string], _ error = sqlite.query(db2, "SELECT content FROM notes;")
    let row map[string]string = rows[0]
    print(row["content"])
    let _ error = sqlite.close(db2)
    return 0
}
`, dbPathEscaped, dbPathEscaped)

	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "hello file\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestSQLiteThreadLocalDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tls_test.db")
	dbPathEscaped := strings.ReplaceAll(dbPath, `\`, `/`)

	src := fmt.Sprintf(`package main
import "sqlite"

func insertRecord() int {
    let db *sqlite.DB, err error = sqlite.openThreadLocal(%q)
    if err != nil {
        return 1
    }
    let _ error = sqlite.exec(db, "INSERT INTO items (val) VALUES ('tls-item');")
    let _ error = sqlite.close(db)
    return 0
}

func main() int {
    let db *sqlite.DB, err error = sqlite.open_thread_local(%q)
    if err != nil {
        return 1
    }
    let _ error = sqlite.exec(db, "CREATE TABLE items (id INTEGER PRIMARY KEY, val TEXT);")

    let _ int = insertRecord()

    let db2 *sqlite.DB, err2 error = sqlite.openThreadLocal(%q)
    if err2 != nil {
        return 1
    }
    let rows list[map[string]string], _ error = sqlite.query(db2, "SELECT val FROM items;")
    let row map[string]string = rows[0]
    print(row["val"])
    return 0
}
`, dbPathEscaped, dbPathEscaped, dbPathEscaped)

	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "tls-item\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestSQLiteAutomaticCleanupOnExit(t *testing.T) {
	src := `package main
import "sqlite"

func main() int {
    let db *sqlite.DB, err error = sqlite.open(":memory:")
    if err != nil {
        return 1
    }
    let _ error = sqlite.exec(db, "CREATE TABLE temp (v INT);")
    // intentionally exit without closing db
    print("exit without explicit close")
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "exit without explicit close\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestSQLiteErrorHandling(t *testing.T) {
	src := `package main
import "sqlite"
import "errors"

func main() int {
    let db *sqlite.DB, _ error = sqlite.open(":memory:")
    let err error = sqlite.exec(db, "SELECT * FROM nonexistent_table;")
    if err != nil {
        print("got expected error:", errors.message(err))
    }
    let _ error = sqlite.close(db)
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	if !strings.Contains(out.String(), "no such table: nonexistent_table") {
		t.Fatalf("unexpected error output: %q", out.String())
	}
}

func TestSQLiteBindNamedAndNull(t *testing.T) {
	src := `package main
import "sqlite"

func main() int {
    let db *sqlite.DB, _ error = sqlite.open(":memory:")
    let _ error = sqlite.exec(db, "CREATE TABLE t (id INTEGER, val TEXT);")

    let stmt *sqlite.Stmt, _ error = sqlite.prepare(db, "INSERT INTO t VALUES (:id, :val);")
    let idIdx int = sqlite.bindIndex(stmt, ":id")
    let valIdx int = sqlite.bindIndex(stmt, ":val")
    print(idIdx)
    print(valIdx)

    let _ error = sqlite.bindInt(stmt, idIdx, 100)
    let _ error = sqlite.bindNull(stmt, valIdx)
    let _, _ error = sqlite.step(stmt)
    let _ error = sqlite.finalize(stmt)

    let qstmt *sqlite.Stmt, _ error = sqlite.prepare(db, "SELECT id, val FROM t WHERE id = 100;")
    let _, _ error = sqlite.step(qstmt)
    print(sqlite.columnIsNull(qstmt, 1))
    print(sqlite.columnType(qstmt, 1))

    let _ error = sqlite.finalize(qstmt)
    let _ error = sqlite.close(db)
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "1\n2\ntrue\nNULL\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestJSONWithoutImportDoesNotCompileJSON(t *testing.T) {
	src := `package main
func main() int {
    print("no json")
    return 0
}
`
	path := writeTempSource(t, src)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.UsesJSON {
		t.Fatalf("expected UsesJSON = false, got true")
	}
	if strings.Contains(result.CSource, "trux_json_runtime.h") {
		t.Fatalf("expected CSource to not include trux_json_runtime.h")
	}
	for _, h := range result.RuntimeHeaders {
		if h.Name == "trux_json_runtime.h" || h.Name == "yyjson.h" {
			t.Fatalf("unexpected json runtime header %s in non-json program", h.Name)
		}
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "program")
	logPath := filepath.Join(dir, "cc.log")
	fakeCC := writeFakeCompiler(t, filepath.Join(dir, "fake-cc"), logPath)
	t.Setenv("CC", fakeCC)
	cPath, err := writeGeneratedFiles(dir, "main", result)
	if err != nil {
		t.Fatal(err)
	}
	if err := compileGenerated(cPath, outputPath, generatedCompileOptions{UsesJSON: result.UsesJSON}); err != nil {
		t.Fatal(err)
	}
	log := readFile(t, logPath)
	if strings.Contains(log, "yyjson") {
		t.Fatalf("expected compiler args to have zero references to yyjson, got: %s", log)
	}
}

func TestJSONParseAndInspect(t *testing.T) {
	src := `package main
import "json"

func main() int {
    let payload string = "{\"name\":\"Alice\",\"age\":30,\"score\":98.5,\"active\":true,\"roles\":[\"admin\",\"editor\"]}"
    let doc *json.Doc, err error = json.parse(payload)
    if err != nil {
        print("parse err")
        return 1
    }
    let root *json.Val = json.root(doc)
    print(json.isObject(root))
    print(json.len(root))

    let nameVal *json.Val = json.get(root, "name")
    print(json.isString(nameVal))
    print(json.getString(nameVal))

    let ageVal *json.Val = json.get(root, "age")
    print(json.isInt(ageVal))
    print(json.getInt(ageVal))

    let scoreVal *json.Val = json.get(root, "score")
    print(json.isFloat(scoreVal))
    print(json.getFloat(scoreVal))

    let activeVal *json.Val = json.get(root, "active")
    print(json.isBool(activeVal))
    print(json.getBool(activeVal))

    let rolesVal *json.Val = json.get(root, "roles")
    print(json.isArray(rolesVal))
    print(json.len(rolesVal))
    let role0 *json.Val = json.at(rolesVal, 0)
    let role1 *json.Val = json.at(rolesVal, 1)
    print(json.getString(role0))
    print(json.getString(role1))

    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "true\n5\ntrue\nAlice\ntrue\n30\ntrue\n98.5\ntrue\ntrue\ntrue\n2\nadmin\neditor\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestJSONFieldHelpers(t *testing.T) {
	src := `package main
import "json"

func main() int {
    let payload string = "{\"title\":\"Trux Compiler\",\"stars\":120,\"rating\":4.9,\"public\":true}"
    let doc *json.Doc, _ error = json.parse(payload)
    let root *json.Val = json.root(doc)

    print(json.fieldString(root, "title"))
    print(json.fieldInt(root, "stars"))
    print(json.fieldFloat(root, "rating"))
    print(json.fieldBool(root, "public"))
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "Trux Compiler\n120\n4.9\ntrue\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestJSONDecodeAndEncodeMap(t *testing.T) {
	src := `package main
import "json"

func main() int {
    let payload string = "{\"city\":\"San Francisco\",\"state\":\"CA\",\"country\":\"USA\"}"
    let m map[string]string, err error = json.decodeMap(payload)
    if err != nil {
        print("decodeMap err")
        return 1
    }
    print(m["city"])
    print(m["state"])
    print(m["country"])

    let encoded string = json.encodeMap(m)
    print(json.valid(encoded))

    let m2 map[string]string, _ error = json.decodeMap(encoded)
    print(m2["city"])
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "San Francisco\nCA\nUSA\ntrue\nSan Francisco\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestJSONMutableBuilder(t *testing.T) {
	src := `package main
import "json"

func main() int {
    let doc *json.MutDoc = json.newDoc()
    let obj *json.MutVal = json.newObject(doc)
    json.setRoot(doc, obj)

    json.addString(doc, obj, "name", "Benchmark Task")
    json.addInt(doc, obj, "iterations", 1000)
    json.addFloat(doc, obj, "latency_ms", 1.25)
    json.addBool(doc, obj, "passed", true)
    json.addNull(doc, obj, "optional")

    let arr *json.MutVal = json.newArray(doc)
    let child1 *json.MutVal = json.newObject(doc)
    json.addString(doc, child1, "tag", "v1")
    json.appendVal(arr, child1)
    json.addVal(doc, obj, "tags", arr)

    let out string = json.write(doc)
    print(json.valid(out))

    let parsed *json.Doc, _ error = json.parse(out)
    let root *json.Val = json.root(parsed)
    print(json.fieldString(root, "name"))
    print(json.fieldInt(root, "iterations"))
    print(json.fieldFloat(root, "latency_ms"))
    print(json.fieldBool(root, "passed"))
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "true\nBenchmark Task\n1000\n1.25\ntrue\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestJSONSyntaxErrorHandling(t *testing.T) {
	src := `package main
import "json"
import "errors"

func main() int {
    let badJSON string = "{\"name\": \"Alice\", invalid}"
    let doc *json.Doc, err error = json.parse(badJSON)
    if err == nil {
        print("expected error, got nil")
        return 1
    }
    let msg string = errors.message(err)
    print(msg)
    print(json.valid(badJSON))
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	if !strings.Contains(out.String(), "json syntax error") {
		t.Fatalf("expected output to contain 'json syntax error', got %q", out.String())
	}
	if !strings.Contains(out.String(), "false") {
		t.Fatalf("expected output to contain 'false' for json.valid, got %q", out.String())
	}
}

func TestJWTSignAndVerify(t *testing.T) {
	src := `package main
import "jwt"

func main() int {
    let claims string = "{\"sub\":\"ada\",\"n\":7,\"admin\":true,\"score\":1.5}"
    let secret string = "0123456789abcdef0123456789abcdef"
    let token string, err error = jwt.sign(claims, secret)
    if err != nil {
        print("sign err")
        return 1
    }
    let parsed *jwt.Claims, verr error = jwt.verify(token, secret)
    if verr != nil {
        print("verify err")
        return 1
    }
    print(jwt.getString(parsed, "sub"))
    print(jwt.getInt(parsed, "n"))
    print(jwt.getBool(parsed, "admin"))
    print(jwt.getFloat(parsed, "score"))
    print(jwt.getString(parsed, "missing"))
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "ada\n7\ntrue\n1.5\n\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestJWTRejectsBadSignature(t *testing.T) {
	src := `package main
import "jwt"

func main() int {
    let claims string = "{\"sub\":\"ada\"}"
    let secret string = "0123456789abcdef0123456789abcdef"
    let other string = "fedcba9876543210fedcba9876543210"
    let token string, err error = jwt.sign(claims, secret)
    if err != nil {
        print("sign err")
        return 1
    }
    let parsed *jwt.Claims, verr error = jwt.verify(token, other)
    if verr != nil {
        print("bad")
    }
    if parsed != nil {
        print("claims")
    }
    let shortToken string, shortErr error = jwt.sign(claims, "secret-key")
    if shortErr != nil {
        print("short-fail")
    } else {
        let _, shortVerify error = jwt.verify(shortToken, "secret-key")
        if shortVerify != nil {
            print("short-fail")
        } else {
            print("short-ok")
        }
    }
    let _, badErr error = jwt.sign("[]", secret)
    if badErr != nil {
        print("not-object")
    }
    let again *jwt.Claims, againErr error = jwt.verify(token, secret)
    if againErr != nil || jwt.getString(again, "sub") != "ada" {
        print("reuse-fail")
    } else {
        print("reuse-ok")
    }
    let _, corrupt error = jwt.verify(token + "x", secret)
    if corrupt != nil {
        print("corrupt")
    }
    let goodAfter *jwt.Claims, goodAfterErr error = jwt.verify(token, secret)
    if goodAfterErr != nil || jwt.getString(goodAfter, "sub") != "ada" {
        print("after-fail")
    } else {
        print("after-ok")
    }
    return 0
}
`
	path := writeTempSource(t, src)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatalf("runFile failed: %v", err)
	}
	want := "bad\nshort-ok\nnot-object\nreuse-ok\ncorrupt\nafter-ok\n"
	if out.String() != want {
		t.Fatalf("got output %q, want %q", out.String(), want)
	}
}

func TestJWTLinkFlags(t *testing.T) {
	plain := writeTempSource(t, `package main
func main() int {
    print("no jwt")
    return 0
}
`)
	plainResult, err := compileFile(plain, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plainResult.UsesJWT {
		t.Fatal("expected UsesJWT = false")
	}
	if strings.Contains(plainResult.CSource, "trux_jwt_runtime.h") {
		t.Fatal("expected CSource to omit trux_jwt_runtime.h")
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "cc.log")
	fakeCC := writeFakeCompiler(t, filepath.Join(dir, "fake-cc"), logPath)
	t.Setenv("CC", fakeCC)
	cPath, err := writeGeneratedFiles(dir, "main", plainResult)
	if err != nil {
		t.Fatal(err)
	}
	if err := compileGenerated(cPath, filepath.Join(dir, "program"), generatedCompileOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, logPath), "-ljwt") {
		t.Fatalf("expected compiler args to omit -ljwt, got: %s", readFile(t, logPath))
	}

	src := `package main
import "jwt"

func main() int {
    let token string, err error = jwt.sign("{\"sub\":\"ada\"}", "0123456789abcdef0123456789abcdef")
    if err != nil {
        return 1
    }
    print(token)
    return 0
}
`
	path := writeTempSource(t, src)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.UsesJWT {
		t.Fatal("expected UsesJWT = true")
	}
	if !strings.Contains(result.CSource, "rt_jwt_sign") {
		t.Fatal("expected generated C to call rt_jwt_sign")
	}
	jwtDir := t.TempDir()
	jwtLog := filepath.Join(jwtDir, "cc.log")
	jwtCC := writeFakeCompiler(t, filepath.Join(jwtDir, "fake-cc"), jwtLog)
	t.Setenv("CC", jwtCC)
	jwtC, err := writeGeneratedFiles(jwtDir, "main", result)
	if err != nil {
		t.Fatal(err)
	}
	if err := compileGenerated(jwtC, filepath.Join(jwtDir, "program"), generatedCompileOptions{UsesJWT: result.UsesJWT}); err != nil {
		t.Fatal(err)
	}
	log := readFile(t, jwtLog)
	if !strings.Contains(log, "-ljwt") {
		t.Fatalf("expected compiler args to contain -ljwt, got: %s", log)
	}
}

func TestRunFileEnvSnapshot(t *testing.T) {
	requireCC(t)

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("TRUX_DOTENV_9F3C_PROC", "proc")
	t.Setenv("TRUX_DOTENV_9F3C_BOTH", "from-proc")
	t.Setenv("TRUX_DOTENV_9F3C_CLEAR", "was-set")
	t.Setenv("TRUX_DOTENV_9F3C_(x86)", "parens")
	lines := []string{
		"# comment",
		"",
		"  # indented comment",
		"TRUX_DOTENV_9F3C_FILE_A=first",
		"TRUX_DOTENV_9F3C_FILE_A=second",
		"TRUX_DOTENV_9F3C_FILE_B=b",
		"TRUX_DOTENV_9F3C_EMPTY=",
		"TRUX_DOTENV_9F3C_EQ=a=b",
		"  TRUX_DOTENV_9F3C_TRIM=kept",
		"TRUX_DOTENV_9F3C_SP= kept",
		"TRUX_DOTENV_9F3C_HASH=a # b",
		"TRUX_DOTENV_9F3C_CASE=upper",
		"TRUX_DOTENV_9F3C_case=lower",
		"TRUX_DOTENV_9F3C_BOTH=from-file",
		"TRUX_DOTENV_9F3C_CLEAR=",
	}
	writeEnvFile(t, dir, strings.Join(lines, "\r\n")+"\r\n")

	path := writeTempSource(t, `package main
import "io"

func load() map[string]string {
    return io.env()
}

func countKey(names list[string], name string) int {
    let n int = 0
    for let i int = 0; i < len(names); i = i + 1 {
        if names[i] == name {
            n = n + 1
        }
    }
    return n
}

func indexOf(names list[string], name string) int {
    for let i int = 0; i < len(names); i = i + 1 {
        if names[i] == name {
            return i
        }
    }
    return -1
}

func emptyKeyCount(names list[string]) int {
    let n int = 0
    for let i int = 0; i < len(names); i = i + 1 {
        if names[i] == "" {
            n = n + 1
        }
    }
    return n
}

func main() int {
    let loaded map[string]string = load()
    let env map[string]string = io.env()
    print("proc=", env["TRUX_DOTENV_9F3C_PROC"])
    print("loaded=", loaded["TRUX_DOTENV_9F3C_PROC"])
    print("both=", env["TRUX_DOTENV_9F3C_BOTH"])
    print("clear=[", env["TRUX_DOTENV_9F3C_CLEAR"], "]")
    print("parens=", env["TRUX_DOTENV_9F3C_(x86)"])
    print("a=", env["TRUX_DOTENV_9F3C_FILE_A"])
    print("b=", env["TRUX_DOTENV_9F3C_FILE_B"])
    print("empty=[", env["TRUX_DOTENV_9F3C_EMPTY"], "]")
    print("eq=", env["TRUX_DOTENV_9F3C_EQ"])
    print("trim=", env["TRUX_DOTENV_9F3C_TRIM"])
    print("sp=[", env["TRUX_DOTENV_9F3C_SP"], "]")
    print("hash=", env["TRUX_DOTENV_9F3C_HASH"])
    print("missing=[", env["TRUX_DOTENV_9F3C_MISSING"], "]")
    print("case=", env["TRUX_DOTENV_9F3C_CASE"])
    print("lower=", env["TRUX_DOTENV_9F3C_case"])
    let names list[string] = keys(env)
    print("count=", countKey(names, "TRUX_DOTENV_9F3C_FILE_A"))
    print("count-both=", countKey(names, "TRUX_DOTENV_9F3C_BOTH"))
    print("count-clear=", countKey(names, "TRUX_DOTENV_9F3C_CLEAR"))
    print("empty-keys=", emptyKeyCount(names))
    print("ipa=", indexOf(names, "TRUX_DOTENV_9F3C_PROC"))
    print("ipb=", indexOf(names, "TRUX_DOTENV_9F3C_BOTH"))
    print("ic=", indexOf(names, "TRUX_DOTENV_9F3C_CLEAR"))
    print("ix=", indexOf(names, "TRUX_DOTENV_9F3C_(x86)"))
    print("ia=", indexOf(names, "TRUX_DOTENV_9F3C_FILE_A"))
    print("ib=", indexOf(names, "TRUX_DOTENV_9F3C_FILE_B"))
    env["TRUX_DOTENV_9F3C_FILE_A"] = "changed"
    let again map[string]string = io.env()
    print("again=", again["TRUX_DOTENV_9F3C_FILE_A"])
    return 0
}`)

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"proc=proc\n",
		"loaded=proc\n",
		"both=from-file\n",
		"a=second\n",
		"b=b\n",
		"empty=[]\n",
		"eq=a=b\n",
		"trim=kept\n",
		"sp=[ kept]\n",
		"hash=a # b\n",
		"clear=[]\n",
		"parens=parens\n",
		"missing=[]\n",
		"case=upper\n",
		"lower=lower\n",
		"count=1\n",
		"count-both=1\n",
		"count-clear=1\n",
		"empty-keys=0\n",
		"again=second\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	ipa := envIndex(t, got, "ipa=")
	ipb := envIndex(t, got, "ipb=")
	ic := envIndex(t, got, "ic=")
	ix := envIndex(t, got, "ix=")
	ia := envIndex(t, got, "ia=")
	ib := envIndex(t, got, "ib=")
	if ipa < 0 || ipb < 0 || ic < 0 || ix < 0 || ia < 0 || ib < 0 {
		t.Fatalf("missing key in snapshot indexes ipa=%d ipb=%d ic=%d ix=%d ia=%d ib=%d\n%s", ipa, ipb, ic, ix, ia, ib, got)
	}
	if !(ipa < ia && ipb < ia && ic < ia && ix < ia && ia < ib) {
		t.Fatalf("key order ipa=%d ipb=%d ic=%d ix=%d ia=%d ib=%d, want process slots before new file keys\n%s", ipa, ipb, ic, ix, ia, ib, got)
	}
}

func TestRunFileEnvRejectsBadFileBeforeMain(t *testing.T) {
	requireCC(t)

	exe := buildEnvProgram(t, `package main
import "io"

func main() int {
    print("started")
    let env map[string]string = io.env()
    print(env["A"])
    return 0
}`)

	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "invalid line", body: "not valid\n", want: ".env:1: invalid line"},
		{name: "invalid key", body: "export FOO=bar\n", want: ".env:1: invalid key"},
		{name: "digit key", body: "1FOO=bar\n", want: ".env:1: invalid key"},
		{name: "later line", body: "# comment\n\nTRUX_DOTENV_9F3C_OK=1\nnot valid\n", want: ".env:4: invalid line"},
		{name: "crlf line number", body: "TRUX_DOTENV_9F3C_OK=1\r\n\r\nnot valid\r\n", want: ".env:3: invalid line"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			writeEnvFile(t, dir, tt.body)
			assertBuiltEnvFailure(t, exe, tt.want)
		})
	}
	t.Run("directory", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		if err := os.Mkdir(filepath.Join(dir, ".env"), 0o755); err != nil {
			t.Fatal(err)
		}
		assertBuiltEnvFailure(t, exe, ".env: not a regular file")
	})
	t.Run("unused function still loads", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		writeEnvFile(t, dir, "not valid\n")
		unused := buildEnvProgram(t, `package main
import "io"

func hidden() map[string]string {
    return io.env()
}

func main() int {
    print("started")
    return 0
}`)
		assertBuiltEnvFailure(t, unused, ".env:1: invalid line")
	})
}

func TestRunFileEnvSkipsHarmlessFile(t *testing.T) {
	requireCC(t)

	exe := buildEnvProgram(t, `package main
import "io"

func emptyKeyCount(names list[string]) int {
    let n int = 0
    for let i int = 0; i < len(names); i = i + 1 {
        if names[i] == "" {
            n = n + 1
        }
    }
    return n
}

func main() int {
    let env map[string]string = io.env()
    let proc string, procOK bool = env["TRUX_DOTENV_9F3C_PROC"]
    let hidden string, hiddenOK bool = env["TRUX_DOTENV_9F3C_HIDDEN"]
    print("proc=", proc)
    print("proc-ok=", procOK)
    print("hidden=[", hidden, "]")
    print("hidden-ok=", hiddenOK)
    let names list[string] = keys(env)
    print("empty-keys=", emptyKeyCount(names))
    return 0
}`)
	want := "proc=proc\nproc-ok=true\nhidden=[]\nhidden-ok=false\nempty-keys=0\n"
	cases := []struct {
		name  string
		write bool
		body  string
	}{
		{name: "missing"},
		{name: "empty", write: true},
		{name: "comments and whitespace", write: true, body: "# TRUX_DOTENV_9F3C_HIDDEN=no\n\n  \n\t\n  # still hidden\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("TRUX_DOTENV_9F3C_PROC", "proc")
			if tt.write {
				writeEnvFile(t, dir, tt.body)
			}
			got, err := runBuiltProgram(exe)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestRunFileEnvAccess(t *testing.T) {
	requireCC(t)

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("TRUX_DOTENV_9F3C_PROC", "proc")
	t.Setenv("TRUX_DOTENV_9F3C_BOTH", "from-proc")
	writeEnvFile(t, dir, "TRUX_DOTENV_9F3C_FILE=file-value\nTRUX_DOTENV_9F3C_BOTH=from-file\n")

	path := writeTempSource(t, `package main
import "io"

func readKey(env map[string]string, name string) string {
    let value string, ok bool = env[name]
    if ok {
        return value
    }
    return ""
}

func indexOf(names list[string], name string) int {
    for let i int = 0; i < len(names); i = i + 1 {
        if names[i] == name {
            return i
        }
    }
    return -1
}

func countKey(names list[string], name string) int {
    let n int = 0
    for let i int = 0; i < len(names); i = i + 1 {
        if names[i] == name {
            n = n + 1
        }
    }
    return n
}

func main() int {
    let env map[string]string = io.env()
    let procName string = "TRUX_DOTENV_9F3C_PROC"
    let fileName string = "TRUX_DOTENV_9F3C_FILE"
    let bothName string = "TRUX_DOTENV_9F3C_BOTH"
    let missingName string = "TRUX_DOTENV_9F3C_MISSING"
    let proc string, procOK bool = env[procName]
    let file string, fileOK bool = env[fileName]
    let both string, bothOK bool = env[bothName]
    let missing string, missingOK bool = env[missingName]
    print("proc=", proc)
    print("proc-ok=", procOK)
    print("file=", file)
    print("file-ok=", fileOK)
    print("both=", both)
    print("both-ok=", bothOK)
    print("missing=[", missing, "]")
    print("missing-ok=", missingOK)
    print("read-proc=", readKey(env, procName))
    print("read-file=", readKey(env, fileName))
    print("read-missing=[", readKey(env, missingName), "]")
    if proc == "proc" && file == "file-value" && both == "from-file" {
        print("compare=yes")
    } else {
        print("compare=no")
    }
    let names list[string] = keys(env)
    let vals list[string] = values(env)
    if len(env) == len(names) && len(names) == len(vals) {
        print("len=match")
    } else {
        print("len=mismatch")
    }
    print("file-at=", vals[indexOf(names, fileName)])
    print("proc-at=", vals[indexOf(names, procName)])
    print("count-both=", countKey(names, bothName))
    print("ib=", indexOf(names, bothName))
    print("if=", indexOf(names, fileName))
    let before int = len(env)
    delete(env, fileName)
    let after string, afterOK bool = env[fileName]
    print("after-ok=", afterOK)
    print("after=[", after, "]")
    print("deleted=", before - len(env))
    let fresh map[string]string = io.env()
    let again string, againOK bool = fresh[fileName]
    print("again=", again)
    print("again-ok=", againOK)
    print("still=", fresh[procName])
    if len(fresh) == before {
        print("fresh-len=same")
    } else {
        print("fresh-len=different")
    }
    return 0
}`)

	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"proc=proc\n",
		"proc-ok=true\n",
		"file=file-value\n",
		"file-ok=true\n",
		"both=from-file\n",
		"both-ok=true\n",
		"missing=[]\n",
		"missing-ok=false\n",
		"read-proc=proc\n",
		"read-file=file-value\n",
		"read-missing=[]\n",
		"compare=yes\n",
		"len=match\n",
		"file-at=file-value\n",
		"proc-at=proc\n",
		"count-both=1\n",
		"after-ok=false\n",
		"after=[]\n",
		"deleted=1\n",
		"again=file-value\n",
		"again-ok=true\n",
		"still=proc\n",
		"fresh-len=same\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	ib := envIndex(t, got, "ib=")
	ifile := envIndex(t, got, "if=")
	if !(ib < ifile) {
		t.Fatalf("overlay index %d, file index %d, want the process slot kept ahead of the new file key\n%s", ib, ifile, got)
	}
}

func TestRunFileEnvLoadsOnce(t *testing.T) {
	requireCC(t)

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("TRUX_DOTENV_9F3C_PROC", "proc")
	writeEnvFile(t, dir, "TRUX_DOTENV_9F3C_FILE=first\n")
	path := writeTempSource(t, `package main
import "io"

func main() int {
    let env map[string]string = io.env()
    print("first=", env["TRUX_DOTENV_9F3C_FILE"])
    print("proc=", env["TRUX_DOTENV_9F3C_PROC"])
    let err error = io.writeFile(".env", "not valid\n")
    if err != nil {
        print("write-failed")
        return 1
    }
    let again map[string]string = io.env()
    print("second=", again["TRUX_DOTENV_9F3C_FILE"])
    print("proc2=", again["TRUX_DOTENV_9F3C_PROC"])
    return 0
}`)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "first=first\nproc=proc\nsecond=first\nproc2=proc\n" {
		t.Fatalf("output = %q", out.String())
	}
	if got := readFile(t, filepath.Join(dir, ".env")); got != "not valid\n" {
		t.Fatalf(".env after run = %q, want the rewritten file", got)
	}
}

func TestRunFileEnvReadsOnlyStartDirectory(t *testing.T) {
	requireCC(t)

	exe := buildEnvProgram(t, `package main
import "io"

func main() int {
    let env map[string]string = io.env()
    let where string, whereOK bool = env["TRUX_DOTENV_9F3C_WHERE"]
    let file string, fileOK bool = env["TRUX_DOTENV_9F3C_FILE"]
    let nested string, nestedOK bool = env["TRUX_DOTENV_9F3C_NESTED"]
    print("proc=", env["TRUX_DOTENV_9F3C_PROC"])
    print("where=", where)
    print("where-ok=", whereOK)
    print("file=", file)
    print("file-ok=", fileOK)
    print("nested=[", nested, "]")
    print("nested-ok=", nestedOK)
    return 0
}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(exe), ".env"), []byte("not valid\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("cwd file wins", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv("TRUX_DOTENV_9F3C_PROC", "proc")
		t.Setenv("TRUX_DOTENV_9F3C_WHERE", "from-proc")
		writeEnvFile(t, dir, "TRUX_DOTENV_9F3C_WHERE=from-cwd\nTRUX_DOTENV_9F3C_FILE=cwd\n")
		nested := filepath.Join(dir, "nested")
		if err := os.Mkdir(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nested, ".env"), []byte("TRUX_DOTENV_9F3C_NESTED=yes\nTRUX_DOTENV_9F3C_WHERE=from-nested\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		beside := filepath.Join(dir, "src")
		if err := os.Mkdir(beside, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(beside, ".env"), []byte("not valid\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := runBuiltProgram(exe)
		if err != nil {
			t.Fatal(err)
		}
		want := "proc=proc\nwhere=from-cwd\nwhere-ok=true\nfile=cwd\nfile-ok=true\nnested=[]\nnested-ok=false\n"
		if got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	})
	t.Run("bad file outside cwd is ignored", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv("TRUX_DOTENV_9F3C_PROC", "proc")
		t.Setenv("TRUX_DOTENV_9F3C_WHERE", "from-proc")
		nested := filepath.Join(dir, "nested")
		if err := os.Mkdir(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nested, ".env"), []byte("not valid\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := runBuiltProgram(exe)
		if err != nil {
			t.Fatal(err)
		}
		want := "proc=proc\nwhere=from-proc\nwhere-ok=true\nfile=\nfile-ok=false\nnested=[]\nnested-ok=false\n"
		if got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	})
}

func TestRunFileWithoutEnvIgnoresBadDotenv(t *testing.T) {
	requireCC(t)

	dir := t.TempDir()
	t.Chdir(dir)
	writeEnvFile(t, dir, "not valid\n")
	path := writeTempSource(t, `package main
func main() int {
    print("ok")
    return 0
}`)
	var out bytes.Buffer
	if err := runFile(&out, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ok\n" {
		t.Fatalf("output = %q, want ok", out.String())
	}
}

func buildEnvProgram(t *testing.T, src string) string {
	t.Helper()
	requireCC(t)

	path := writeTempSource(t, src)
	result, err := compileFile(path, compileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cPath, err := writeGeneratedFiles(dir, "main", result)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "main")
	if err := compileGenerated(cPath, exe, generatedCompileOptions{}); err != nil {
		t.Fatal(err)
	}
	return exe
}

func runBuiltProgram(exe string) (string, error) {
	cmd := exec.Command(exe)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return stdout.String(), fmt.Errorf("execute %s: %w", exe, err)
		}
		return stdout.String(), fmt.Errorf("execute %s: %w\n%s", exe, err, msg)
	}
	return stdout.String(), nil
}

func assertBuiltEnvFailure(t *testing.T, exe string, want string) {
	t.Helper()
	out, err := runBuiltProgram(exe)
	if err == nil {
		t.Fatal("expected env load to fail")
	}
	if out != "" {
		t.Fatalf("output = %q, want empty output before main", out)
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("error = %q, want exit status 1", err.Error())
	}
	if !strings.Contains(err.Error(), "trux runtime error: "+want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func writeEnvFile(t *testing.T, dir string, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func envIndex(t *testing.T, out string, prefix string) int {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			n, err := strconv.Atoi(strings.TrimPrefix(line, prefix))
			if err != nil {
				t.Fatalf("index %q: %v", line, err)
			}
			return n
		}
	}
	t.Fatalf("output missing %q:\n%s", prefix, out)
	return -1
}
