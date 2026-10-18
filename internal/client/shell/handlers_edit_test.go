package shell

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"dfsha/internal/client/api"
)

// fakeFiles es un servidor de archivos en memoria que registra las subidas.
type fakeFiles struct {
	content    map[string][]byte
	replaceErr error
	putErr     error

	puts     map[string][]byte
	replaces map[string][]byte
	baseSHA  string
}

func newFakeFiles(initial map[string]string) *fakeFiles {
	f := &fakeFiles{
		content:  map[string][]byte{},
		puts:     map[string][]byte{},
		replaces: map[string][]byte{},
	}
	for k, v := range initial {
		f.content[k] = []byte(v)
	}
	return f
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (f *fakeFiles) Download(remote, local, token string, maxSize int64) (api.FileInfo, error) {
	data, ok := f.content[remote]
	if !ok {
		return api.FileInfo{}, errors.Join(errors.New("download rejected"), &api.Error{Status: http.StatusNotFound, Message: "not found"})
	}
	if maxSize > 0 && int64(len(data)) > maxSize {
		return api.FileInfo{}, &api.TooLargeError{Size: int64(len(data)), Max: maxSize}
	}
	if err := os.WriteFile(local, data, 0o600); err != nil {
		return api.FileInfo{}, err
	}
	return api.FileInfo{SizeBytes: int64(len(data)), SHA256: sha(data)}, nil
}

func (f *fakeFiles) Put(local, remote, token string) error {
	if f.putErr != nil {
		return f.putErr
	}
	data, _ := os.ReadFile(local)
	f.puts[remote] = data
	return nil
}

func (f *fakeFiles) Replace(local, remote, base, token string) error {
	f.baseSHA = base
	if f.replaceErr != nil {
		return f.replaceErr
	}
	data, _ := os.ReadFile(local)
	f.replaces[remote] = data
	return nil
}

// useTempDir apunta el directorio temporal a uno de prueba para comprobar la limpieza.
func useTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("el directorio temporal debía quedar vacío, hay %d entradas", len(entries))
	}
}

// editorThat devuelve un editor falso que aplica fn al contenido.
func editorThat(fn func(string) string) editorRunner {
	return func(argv []string, file string) error {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		return os.WriteFile(file, []byte(fn(string(data))), 0o600)
	}
}

func neverRun(t *testing.T) editorRunner {
	return func([]string, string) error {
		t.Fatalf("el editor no debía ejecutarse")
		return nil
	}
}

// --- cat ---

func TestCatImprimeElContenido(t *testing.T) {
	tmp := useTempDir(t)
	files := newFakeFiles(map[string]string{"/a.txt": "hola\nmundo"})

	var out bytes.Buffer
	if err := catFile(files, "tok", "/a.txt", &out); err != nil {
		t.Fatalf("catFile falló: %v", err)
	}
	if out.String() != "hola\nmundo\n" {
		t.Fatalf("salida = %q", out.String())
	}
	assertEmptyDir(t, tmp)
}

func TestCatOcultaCaracteresDeControl(t *testing.T) {
	useTempDir(t)
	files := newFakeFiles(map[string]string{"/a.txt": "ok\x1b[31mrojo\x1b[0m\r\nfin\n"})

	var out bytes.Buffer
	if err := catFile(files, "tok", "/a.txt", &out); err != nil {
		t.Fatalf("catFile falló: %v", err)
	}
	if strings.ContainsRune(out.String(), 0x1b) {
		t.Fatalf("la salida no debe contener ESC: %q", out.String())
	}
	if !strings.Contains(out.String(), "ok[31mrojo[0m\nfin\n") {
		t.Fatalf("el texto debía conservarse sin los controles: %q", out.String())
	}
	if !strings.Contains(out.String(), "2 control characters were hidden") {
		t.Fatalf("faltó el aviso de caracteres ocultos: %q", out.String())
	}
}

func TestCatNoAvisaPorSaltosDeLineaDeWindows(t *testing.T) {
	useTempDir(t)
	files := newFakeFiles(map[string]string{"/a.txt": "uno\r\ndos\r\n"})

	var out bytes.Buffer
	if err := catFile(files, "tok", "/a.txt", &out); err != nil {
		t.Fatalf("catFile falló: %v", err)
	}
	if out.String() != "uno\ndos\n" {
		t.Fatalf("salida = %q", out.String())
	}
}

func TestCatRechazaBinarios(t *testing.T) {
	tmp := useTempDir(t)
	files := newFakeFiles(map[string]string{"/b.bin": "abc\x00def"})

	var out bytes.Buffer
	err := catFile(files, "tok", "/b.bin", &out)
	if !errors.Is(err, errBinaryFile) {
		t.Fatalf("error = %v, se esperaba errBinaryFile", err)
	}
	if out.Len() != 0 {
		t.Fatalf("no debía imprimirse nada: %q", out.String())
	}
	assertEmptyDir(t, tmp)
}

func TestCatRechazaArchivosGrandes(t *testing.T) {
	useTempDir(t)
	files := newFakeFiles(map[string]string{"/big.txt": strings.Repeat("a", maxCatBytes+1)})

	var out bytes.Buffer
	err := catFile(files, "tok", "/big.txt", &out)
	if err == nil || !strings.Contains(err.Error(), "use 'get'") {
		t.Fatalf("error = %v, se esperaba sugerir get", err)
	}
	if out.Len() != 0 {
		t.Fatalf("no debía imprimirse nada")
	}
}

func TestCatArchivoInexistente(t *testing.T) {
	useTempDir(t)
	files := newFakeFiles(nil)

	var out bytes.Buffer
	if err := catFile(files, "tok", "/no.txt", &out); !isNotFound(err) {
		t.Fatalf("error = %v, se esperaba 404", err)
	}
}

// --- edit ---

func TestEditSubeLosCambiosConLaVersionBase(t *testing.T) {
	tmp := useTempDir(t)
	files := newFakeFiles(map[string]string{"/docs/a.txt": "original"})

	editor := func(argv []string, file string) error {
		// El temporal debe ser privado mientras se edita.
		st, err := os.Stat(file)
		if err != nil {
			return err
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("permisos del temporal = %o, se esperaba 600", st.Mode().Perm())
		}
		dst, _ := os.Stat(filepath.Dir(file))
		if dst.Mode().Perm() != 0o700 {
			t.Errorf("permisos del directorio = %o, se esperaba 700", dst.Mode().Perm())
		}
		if filepath.Base(file) != "a.txt" {
			t.Errorf("el temporal debía conservar el nombre, es %q", filepath.Base(file))
		}
		return editorThat(func(s string) string { return s + " editado" })(argv, file)
	}

	var out bytes.Buffer
	if err := editFile(files, "tok", "/docs/a.txt", []string{"x"}, editor, &out); err != nil {
		t.Fatalf("editFile falló: %v", err)
	}

	if string(files.replaces["/docs/a.txt"]) != "original editado" {
		t.Fatalf("contenido subido = %q", files.replaces["/docs/a.txt"])
	}
	if files.baseSHA != sha([]byte("original")) {
		t.Fatalf("la versión base debía ser el hash del original, fue %q", files.baseSHA)
	}
	if len(files.puts) != 0 {
		t.Fatalf("no debía usar Put en un archivo existente")
	}
	if !strings.Contains(out.String(), "Saved /docs/a.txt") {
		t.Fatalf("salida = %q", out.String())
	}
	assertEmptyDir(t, tmp)
}

func TestEditSinCambiosNoSube(t *testing.T) {
	tmp := useTempDir(t)
	files := newFakeFiles(map[string]string{"/a.txt": "igual"})

	var out bytes.Buffer
	err := editFile(files, "tok", "/a.txt", []string{"x"}, editorThat(func(s string) string { return s }), &out)
	if err != nil {
		t.Fatalf("editFile falló: %v", err)
	}
	if len(files.replaces) != 0 || len(files.puts) != 0 {
		t.Fatalf("no debía subirse nada")
	}
	if !strings.Contains(out.String(), "No changes") {
		t.Fatalf("salida = %q", out.String())
	}
	assertEmptyDir(t, tmp)
}

func TestEditCreaArchivoNuevo(t *testing.T) {
	tmp := useTempDir(t)
	files := newFakeFiles(nil)

	var out bytes.Buffer
	err := editFile(files, "tok", "/nuevo.txt", []string{"x"}, editorThat(func(string) string { return "contenido" }), &out)
	if err != nil {
		t.Fatalf("editFile falló: %v", err)
	}
	if string(files.puts["/nuevo.txt"]) != "contenido" {
		t.Fatalf("Put recibió %q", files.puts["/nuevo.txt"])
	}
	if len(files.replaces) != 0 {
		t.Fatalf("un archivo nuevo no debe usar Replace")
	}
	assertEmptyDir(t, tmp)
}

func TestEditArchivoNuevoVacioNoSube(t *testing.T) {
	useTempDir(t)
	files := newFakeFiles(nil)

	var out bytes.Buffer
	err := editFile(files, "tok", "/nuevo.txt", []string{"x"}, editorThat(func(s string) string { return s }), &out)
	if err != nil {
		t.Fatalf("editFile falló: %v", err)
	}
	if len(files.puts) != 0 {
		t.Fatalf("no debía subirse un archivo vacío")
	}
}

func TestEditRechazaVaciarUnArchivo(t *testing.T) {
	tmp := useTempDir(t)
	files := newFakeFiles(map[string]string{"/a.txt": "algo"})

	var out bytes.Buffer
	err := editFile(files, "tok", "/a.txt", []string{"x"}, editorThat(func(string) string { return "" }), &out)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error = %v, se esperaba rechazo por vacío", err)
	}
	if len(files.replaces) != 0 {
		t.Fatalf("no debía subirse nada")
	}
	assertEmptyDir(t, tmp)
}

func TestEditRechazaBinariosSinAbrirElEditor(t *testing.T) {
	tmp := useTempDir(t)
	files := newFakeFiles(map[string]string{"/b.bin": "a\x00b"})

	var out bytes.Buffer
	err := editFile(files, "tok", "/b.bin", []string{"x"}, neverRun(t), &out)
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("error = %v, se esperaba rechazo por binario", err)
	}
	assertEmptyDir(t, tmp)
}

func TestEditRechazaArchivosGrandesSinAbrirElEditor(t *testing.T) {
	tmp := useTempDir(t)
	files := newFakeFiles(map[string]string{"/big.txt": strings.Repeat("a", maxEditBytes+1)})

	var out bytes.Buffer
	err := editFile(files, "tok", "/big.txt", []string{"x"}, neverRun(t), &out)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v, se esperaba rechazo por tamaño", err)
	}
	assertEmptyDir(t, tmp)
}

func TestEditRechazaSiElEditadoCrecioDemasiado(t *testing.T) {
	useTempDir(t)
	files := newFakeFiles(map[string]string{"/a.txt": "x"})

	big := strings.Repeat("a", maxEditBytes+1)
	var out bytes.Buffer
	err := editFile(files, "tok", "/a.txt", []string{"x"}, editorThat(func(string) string { return big }), &out)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v, se esperaba rechazo por tamaño", err)
	}
	if len(files.replaces) != 0 {
		t.Fatalf("no debía subirse nada")
	}
}

func TestEditSiFallaElEditorNoSube(t *testing.T) {
	tmp := useTempDir(t)
	files := newFakeFiles(map[string]string{"/a.txt": "x"})

	failing := func([]string, string) error { return errors.New("exit status 1") }
	var out bytes.Buffer
	err := editFile(files, "tok", "/a.txt", []string{"x"}, failing, &out)
	if err == nil || !strings.Contains(err.Error(), "nothing was uploaded") {
		t.Fatalf("error = %v", err)
	}
	if len(files.replaces) != 0 {
		t.Fatalf("no debía subirse nada")
	}
	assertEmptyDir(t, tmp)
}

func TestEditConflictoGuardaCopiaDeRecuperacion(t *testing.T) {
	tmp := useTempDir(t)
	cwd := t.TempDir()
	t.Chdir(cwd)

	files := newFakeFiles(map[string]string{"/a.txt": "base"})
	files.replaceErr = &api.Error{Status: http.StatusConflict, Message: "cambió"}

	var out bytes.Buffer
	err := editFile(files, "tok", "/a.txt", []string{"x"}, editorThat(func(string) string { return "mi trabajo" }), &out)
	if err == nil || !strings.Contains(err.Error(), "changed on the server") {
		t.Fatalf("error = %v, se esperaba aviso de conflicto", err)
	}
	if !strings.Contains(err.Error(), "a.txt.dfsha-recovery") {
		t.Fatalf("el error debía indicar la copia de recuperación: %v", err)
	}

	got, rerr := os.ReadFile(filepath.Join(cwd, "a.txt.dfsha-recovery"))
	if rerr != nil || string(got) != "mi trabajo" {
		t.Fatalf("copia de recuperación = %q (err=%v)", got, rerr)
	}
	st, _ := os.Stat(filepath.Join(cwd, "a.txt.dfsha-recovery"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("permisos de la copia = %o, se esperaba 600", st.Mode().Perm())
	}
	assertEmptyDir(t, tmp)
}

func TestEditErrorDeSubidaGuardaCopia(t *testing.T) {
	useTempDir(t)
	cwd := t.TempDir()
	t.Chdir(cwd)

	files := newFakeFiles(nil)
	files.putErr = errors.New("sin red")

	var out bytes.Buffer
	err := editFile(files, "tok", "/n.txt", []string{"x"}, editorThat(func(string) string { return "texto" }), &out)
	if err == nil || !strings.Contains(err.Error(), "upload failed") || !strings.Contains(err.Error(), "n.txt.dfsha-recovery") {
		t.Fatalf("error = %v", err)
	}
	if _, rerr := os.Stat(filepath.Join(cwd, "n.txt.dfsha-recovery")); rerr != nil {
		t.Fatalf("faltó la copia de recuperación: %v", rerr)
	}
}

func TestSaveRecoveryNoPisaCopiasPrevias(t *testing.T) {
	t.Chdir(t.TempDir())
	src := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(src, []byte("uno"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := saveRecovery(src, "x")
	if err != nil {
		t.Fatalf("primera copia falló: %v", err)
	}
	if err := os.WriteFile(src, []byte("dos"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := saveRecovery(src, "x")
	if err != nil {
		t.Fatalf("segunda copia falló: %v", err)
	}

	if first == second {
		t.Fatalf("la segunda copia no debe pisar la primera: %q", first)
	}
	if got, _ := os.ReadFile(first); string(got) != "uno" {
		t.Fatalf("la primera copia cambió: %q", got)
	}
}

// --- editor, binarios y sanitización ---

func TestResolveEditor(t *testing.T) {
	found := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, f := range names {
				if f == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	env := func(v string) func(string) string {
		return func(string) string { return v }
	}

	tests := []struct {
		name    string
		forced  string
		env     string
		have    []string
		want    []string
		wantErr bool
	}{
		{name: "forzado y presente", forced: "nano", have: []string{"nano"}, want: []string{"nano"}},
		{name: "forzado y ausente", forced: "nano", have: []string{"vi"}, wantErr: true},
		{name: "EDITOR con argumentos", env: "code -w", have: nil, want: []string{"code", "-w"}},
		{name: "EDITOR gana sobre nano", env: "vim", have: []string{"nano"}, want: []string{"vim"}},
		{name: "sin EDITOR usa nano", have: []string{"nano", "vi"}, want: []string{"nano"}},
		{name: "sin nano usa vi", have: []string{"vi"}, want: []string{"vi"}},
		{name: "nada disponible", have: nil, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveEditor(tc.forced, env(tc.env), found(tc.have...))
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("editor = %v, se esperaba %v", got, tc.want)
			}
		})
	}
}

func TestLooksBinary(t *testing.T) {
	if looksBinary([]byte("texto normal con ñ y tildes")) {
		t.Fatalf("el texto UTF-8 no es binario")
	}
	if !looksBinary([]byte("a\x00b")) {
		t.Fatalf("un byte nulo debe detectarse")
	}
	// Un nulo después del primer bloque no cuenta.
	late := append(bytes.Repeat([]byte("a"), binaryProbeBytes), 0)
	if looksBinary(late) {
		t.Fatalf("solo se revisa el primer bloque")
	}
}

func TestSafeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"a.txt": "a.txt", "..": "file", ".": "file", "/": "file", "": "file",
	} {
		if got := safeFileName(in); got != want {
			t.Errorf("safeFileName(%q) = %q, se esperaba %q", in, got, want)
		}
	}
}

// --- ejecución real de un proceso editor ---

func TestRunEditorProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requiere un script de shell")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ' via-script' >> \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(file, []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runEditorProcess([]string{script}, file); err != nil {
		t.Fatalf("runEditorProcess falló: %v", err)
	}
	if got, _ := os.ReadFile(file); string(got) != "base via-script" {
		t.Fatalf("contenido = %q", got)
	}

	failing := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runEditorProcess([]string{failing}, file); err == nil {
		t.Fatalf("se esperaba error si el editor sale con código distinto de 0")
	}
}

// --- registro de comandos ---

func TestComandosRegistrados(t *testing.T) {
	s := &shell{commands: defaultCommands}
	for _, name := range []string{"cat", "edit", "nano"} {
		cmd := findCommand(s, name)
		if cmd == nil {
			t.Fatalf("el comando %q no está registrado", name)
		}
		if !cmd.requiresAuth {
			t.Fatalf("el comando %q debe requerir sesión", name)
		}
	}
}
