package notify

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// BUG-001: go-toast library produces garbled Chinese characters in Windows toast notifications.
// Root cause: go-toast writes PowerShell scripts with raw UTF-8 bytes via text/template,
// which gets mangled across the Go -> temp file -> PowerShell -> WinRT XML pipeline.
// Fix: bypass go-toast entirely; encode non-ASCII as XML numeric character references
// (&#xxxxx;) and pass the script via PowerShell -EncodedCommand (UTF-16LE base64).

// TC01: XML generation encodes Chinese as numeric character references (pure ASCII output)
func TestTC01_XMLGeneration_ChineseToNumericRef(t *testing.T) {
	title := "中文测试"
	content := "来自串口网关的通知"

	xml := toastXML(title, content)
	t.Logf("Generated XML:\n%s", xml)

	if !strings.Contains(xml, "&#") {
		t.Fatal("Expected XML numeric character references for Chinese characters")
	}

	for _, r := range xml {
		if r > 127 {
			t.Fatalf("Found non-ASCII rune in XML: %c (U+%04X)", r, r)
		}
	}
}

// TC02: XML generation preserves English text verbatim
func TestTC02_XMLGeneration_EnglishVerbatim(t *testing.T) {
	title := "English Test"
	content := "Hello World"

	xml := toastXML(title, content)
	t.Logf("Generated XML:\n%s", xml)

	if !strings.Contains(xml, "English Test") {
		t.Fatal("English title not found verbatim in XML")
	}
	if !strings.Contains(xml, "Hello World") {
		t.Fatal("English content not found verbatim in XML")
	}
}

// TC03: UTF-16LE base64 encoding roundtrips correctly for mixed Chinese/English
func TestTC03_UTF16LEBase64_Roundtrip(t *testing.T) {
	original := "中文测试 Hello"
	encoded := encodeUTF16LEBase64(original)

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}

	var runes []rune
	for i := 0; i < len(decoded); i += 2 {
		r := rune(binary.LittleEndian.Uint16(decoded[i : i+2]))
		runes = append(runes, r)
	}
	result := string(runes)

	if result != original {
		t.Fatalf("Roundtrip failed:\n  original: %q\n  result:   %q", original, result)
	}
}

// TC04: Full pipeline preserves XML numeric references through base64 encoding
func TestTC04_FullPipeline_PreservesReferences(t *testing.T) {
	title := "中文标题"
	content := "中文正文内容，包含标点！"

	xml := toastXML(title, content)
	script := powershellScript(xml)
	encoded := encodeUTF16LEBase64(script)

	// Decode and verify the script still contains numeric references
	decoded, _ := base64.StdEncoding.DecodeString(encoded)
	var runes []rune
	for i := 0; i < len(decoded); i += 2 {
		r := rune(binary.LittleEndian.Uint16(decoded[i : i+2]))
		runes = append(runes, r)
	}
	decodedScript := string(runes)

	if !strings.Contains(decodedScript, "&#") {
		t.Fatal("Decoded script lost the XML numeric references")
	}

	// Write script to temp file for manual inspection if needed
	tmpFile := os.TempDir() + "\\toast_test_script.ps1"
	bom := []byte{0xEF, 0xBB, 0xBF}
	os.WriteFile(tmpFile, append(bom, []byte(decodedScript)...), 0644)
	t.Logf("Script written to: %s", tmpFile)
}

// TC05: PowerShell WinRT XmlDocument correctly decodes numeric character references
// to original Chinese text (requires Windows desktop session)
func TestTC05_PowerShellXMLParse_DecodesChineseCorrectly(t *testing.T) {
	if os.Getenv("RUN_TOAST_TEST") == "" {
		t.Skip("Set RUN_TOAST_TEST=1 to run (requires Windows desktop)")
	}

	title := "中文标题测试"
	content := "中文正文：你好世界！"

	xml := toastXML(title, content)

	verifyScript := fmt.Sprintf(`
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null

$template = @'
%s
'@

$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($template)
$texts = $xml.GetElementsByTagName("text")
foreach ($text in $texts) {
    Write-Output ("TEXT: " + $text.InnerText)
}
`, xml)

	encoded := encodeUTF16LEBase64(verifyScript)

	cmd := exec.Command("PowerShell", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", encoded)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("PowerShell failed: %v\nstderr: %s", err, stderr.String())
	}

	output := stdout.String()
	t.Logf("PowerShell XML parse output:\n%s", output)

	if !strings.Contains(output, title) {
		t.Errorf("Title %q not found in PowerShell output", title)
	}
	if !strings.Contains(output, content) {
		t.Errorf("Content %q not found in PowerShell output", content)
	}
}

// TC06: End-to-end toast dispatch with Chinese content
// (requires Windows desktop session, manual visual verification)
func TestTC06_ActualToast_Chinese(t *testing.T) {
	if os.Getenv("RUN_TOAST_TEST") == "" {
		t.Skip("Set RUN_TOAST_TEST=1 to run (requires Windows desktop)")
	}

	title := "中文测试"
	content := "来自串口网关的 Toast 通知！"

	xml := toastXML(title, content)
	script := powershellScript(xml)
	encoded := encodeUTF16LEBase64(script)

	t.Logf("Sending toast with title=%q content=%q", title, content)

	cmd := exec.Command("PowerShell", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", encoded)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("PowerShell failed: %v\nstderr: %s", err, stderr.String())
	}

	t.Log("Toast dispatched. Verify Chinese text on desktop notification.")
}
