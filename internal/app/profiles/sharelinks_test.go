package profiles

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/singbox"
)

// A vless link with a remark, the shape a provider hands out; the credentials are obviously
// fake and are never asserted on: the tests check structure, not secrets.
const vlessLink = "vless://11111111-2222-3333-4444-555555555555@server.example.com:443" +
	"?type=tcp&security=reality&sni=www.example.org&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" +
	"&sid=0123#Node%20One"

const hysteria2Link = "hy2://secret@other.example.com:8443?sni=other.example.org&insecure=1#Node%20Two"

// document decodes a generated configuration and returns it as a map.
func document(t *testing.T, content string) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		t.Fatalf("the generated configuration is not JSON: %v", err)
	}
	return out
}

// outbounds lists the outbounds of a document with their tags and types.
func outbounds(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	items, ok := doc["outbounds"].([]any)
	if !ok {
		t.Fatalf("outbounds = %#v, want an array", doc["outbounds"])
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("outbound = %#v, want an object", item)
		}
		out = append(out, object)
	}
	return out
}

// tagOf returns the tag of the outbound at index i.
func tagOf(t *testing.T, items []map[string]any, i int) string {
	t.Helper()
	if i >= len(items) {
		t.Fatalf("no outbound at index %d (%d present)", i, len(items))
	}
	tag, _ := items[i]["tag"].(string)
	return tag
}

// finalOf returns route.final.
func finalOf(t *testing.T, doc map[string]any) string {
	t.Helper()
	route, ok := doc["route"].(map[string]any)
	if !ok {
		t.Fatalf("route = %#v, want an object", doc["route"])
	}
	final, _ := route["final"].(string)
	return final
}

// dnsDetourOf returns the detour of the DNS server with the given tag.
func dnsDetourOf(t *testing.T, doc map[string]any, tag string) string {
	t.Helper()
	section, ok := doc["dns"].(map[string]any)
	if !ok {
		return ""
	}
	for _, item := range asAnySlice(section["servers"]) {
		server, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if server["tag"] == tag {
			detour, _ := server["detour"].(string)
			return detour
		}
	}
	return ""
}

func TestGenerateFromShareLinksReplacesThePlaceholderOnTheTunBase(t *testing.T) {
	generated, err := GenerateFromShareLinks(ShareLinkInput{Links: vlessLink})
	if err != nil {
		t.Fatalf("GenerateFromShareLinks() failed: %v", err)
	}
	doc := document(t, generated.ConfigJSON)
	items := outbounds(t, doc)

	// The parsed outbound leads the list, on the tag the remark asked for.
	if got := tagOf(t, items, 0); got != "Node One" {
		t.Errorf("first outbound tag = %q, want the link's remark %q", got, "Node One")
	}
	if kind := items[0]["type"]; kind != "vless" {
		t.Errorf("first outbound type = %v, want vless", kind)
	}
	if server := items[0]["server"]; server != "server.example.com" {
		t.Errorf("first outbound server = %v, want the link's host", server)
	}

	// Nothing of the starter's placeholder survives.
	for _, item := range items {
		for key, value := range item {
			if text, ok := value.(string); ok && strings.HasPrefix(text, "REPLACE_") {
				t.Errorf("outbound %v still carries the placeholder %s=%q", item["tag"], key, text)
			}
		}
	}
	// The routing follows the replacement: the placeholder tag no longer exists anywhere.
	if got := finalOf(t, doc); got != "Node One" {
		t.Errorf("route.final = %q, want the link's tag", got)
	}
	if got := dnsDetourOf(t, doc, "remote"); got != "Node One" {
		t.Errorf("dns remote detour = %q, want the link's tag", got)
	}
	// The rest of the starter is untouched: the fallback outbound, the TUN inbound and the
	// rule that keeps private addresses local.
	if got := tagOf(t, items, len(items)-1); got != "direct" {
		t.Errorf("last outbound tag = %q, want the starter's direct", got)
	}
	if !hasInboundOfType(doc, "tun") {
		t.Error("the generated TUN profile has no tun inbound")
	}
	if !hasRuleRoutingPrivateAddressesDirectly(doc) {
		t.Error("the rule that routes private addresses directly was lost")
	}
	if generated.Count != 1 || generated.Kind != "vless" || generated.Name != "Node One" {
		t.Errorf("summary = %+v, want one vless link named by its remark", generated)
	}
	if len(generated.Warnings) != 0 {
		t.Errorf("warnings = %v, want none for a clean link", generated.Warnings)
	}
}

func TestGenerateFromShareLinksLocalBaseHasNoTun(t *testing.T) {
	generated, err := GenerateFromShareLinks(ShareLinkInput{Links: hysteria2Link, Base: "local"})
	if err != nil {
		t.Fatalf("GenerateFromShareLinks() failed: %v", err)
	}
	doc := document(t, generated.ConfigJSON)
	if hasInboundOfType(doc, "tun") {
		t.Error("the local base generated a TUN inbound, which is exactly what it must not do")
	}
	if !hasInboundOfType(doc, "mixed") {
		t.Error("the local base generated no mixed inbound, so nothing would listen")
	}
	if got := finalOf(t, doc); got != "Node Two" {
		t.Errorf("route.final = %q, want the link's tag", got)
	}
	if got := outbounds(t, doc)[0]["type"]; got != "hysteria2" {
		t.Errorf("first outbound type = %v, want hysteria2", got)
	}
}

func TestGenerateFromShareLinksGroupsSeveralLinks(t *testing.T) {
	generated, err := GenerateFromShareLinks(ShareLinkInput{Links: vlessLink + "\n" + hysteria2Link})
	if err != nil {
		t.Fatalf("GenerateFromShareLinks() failed: %v", err)
	}
	doc := document(t, generated.ConfigJSON)
	items := outbounds(t, doc)

	groupTag := tagOf(t, items, 0)
	if kind := items[0]["type"]; kind != "urltest" {
		t.Fatalf("first outbound type = %v, want the urltest group to lead", kind)
	}
	members, _ := items[0]["outbounds"].([]any)
	if len(members) != 2 || members[0] != "Node One" || members[1] != "Node Two" {
		t.Errorf("group members = %v, want both links", members)
	}
	if got := finalOf(t, doc); got != groupTag {
		t.Errorf("route.final = %q, want the group %q", got, groupTag)
	}
	if got := dnsDetourOf(t, doc, "remote"); got != groupTag {
		t.Errorf("dns remote detour = %q, want the group %q", got, groupTag)
	}
	if generated.Count != 2 {
		t.Errorf("Count = %d, want 2", generated.Count)
	}
	// Both servers are still in the document, after the group.
	if tagOf(t, items, 1) != "Node One" || tagOf(t, items, 2) != "Node Two" {
		t.Errorf("outbound tags = %v, want the two links after the group", items)
	}
}

func TestGenerateFromShareLinksKeepsUnusableLinesAsWarnings(t *testing.T) {
	// The parser reads pasted text word by word -- a subscription usually arrives as one link
	// per line, but a line that carries several is not unusual, and a word that starts no
	// scheme is simply one unusable line for the user to look at.
	paste := "nonsense\n" + vlessLink + "\nss://not-a-real-credential\n"
	generated, err := GenerateFromShareLinks(ShareLinkInput{Links: paste})
	if err != nil {
		t.Fatalf("GenerateFromShareLinks() failed: %v", err)
	}
	if generated.Count != 1 {
		t.Errorf("Count = %d, want the one usable link", generated.Count)
	}
	if len(generated.Warnings) != 2 {
		t.Fatalf("warnings = %v, want one per unusable line", generated.Warnings)
	}
	// The warnings name the line, which is what the user can act on.
	joined := strings.Join(generated.Warnings, "\n")
	if !strings.Contains(joined, "line 1") || !strings.Contains(joined, "line 3") {
		t.Errorf("warnings = %v, want the source lines of the skipped input", generated.Warnings)
	}
	if strings.Contains(joined, "not-a-real-credential") {
		t.Error("a warning repeated text from the pasted link; credentials must never appear")
	}
}

func TestGenerateFromShareLinksRefusesAPasteWithNothingUsable(t *testing.T) {
	_, err := GenerateFromShareLinks(ShareLinkInput{Links: "https://example.com/subscription\nnonsense\n"})
	if err == nil {
		t.Fatal("GenerateFromShareLinks() succeeded, want an error")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeShareLinkInvalid {
		t.Errorf("code = %s, want %s (%v)", code, apperr.CodeShareLinkInvalid, err)
	}
	if details := apperr.DetailsOf(err); len(details) == 0 {
		t.Error("the refusal carries no details, so the dialog cannot say which line failed")
	}
}

func TestGenerateFromShareLinksRefusesAnEmptyPaste(t *testing.T) {
	if _, err := GenerateFromShareLinks(ShareLinkInput{Links: "   \n\n"}); err == nil {
		t.Fatal("GenerateFromShareLinks() succeeded for an empty paste, want an error")
	}
}

func TestGenerateFromShareLinksRefusesAnUnknownBase(t *testing.T) {
	_, err := GenerateFromShareLinks(ShareLinkInput{Links: vlessLink, Base: "wireguard"})
	if err == nil {
		t.Fatal("GenerateFromShareLinks() accepted an unknown base")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeInvalidArgument {
		t.Errorf("code = %s, want %s", code, apperr.CodeInvalidArgument)
	}
}

func TestGenerateFromShareLinksAvoidsTagsTheSkeletonUses(t *testing.T) {
	// A remark that collides with the starter's own fallback outbound must not take its place:
	// two outbounds with one tag is a configuration sing-box refuses.
	link := strings.Replace(vlessLink, "#Node%20One", "#direct", 1)
	generated, err := GenerateFromShareLinks(ShareLinkInput{Links: link})
	if err != nil {
		t.Fatalf("GenerateFromShareLinks() failed: %v", err)
	}
	doc := document(t, generated.ConfigJSON)
	items := outbounds(t, doc)

	seen := map[string]int{}
	for _, item := range items {
		tag, _ := item["tag"].(string)
		seen[tag]++
	}
	for tag, count := range seen {
		if count > 1 {
			t.Errorf("tag %q appears %d times, want unique tags", tag, count)
		}
	}
	proxyTag := tagOf(t, items, 0)
	if proxyTag == "direct" {
		t.Error("the parsed link took the tag of the starter's fallback outbound")
	}
	if got := finalOf(t, doc); got != proxyTag {
		t.Errorf("route.final = %q, want the renamed proxy %q", got, proxyTag)
	}
	if got := tagOf(t, items, len(items)-1); got != "direct" {
		t.Errorf("the fallback outbound is now %q, want the starter's direct", got)
	}
}

// TestShareLinkBasesKeepTheirAnchor is the contract this file relies on: every declared base is
// a starter that routes through the placeholder outbound, so replacing it gives the profile the
// server from the link.
func TestShareLinkBasesKeepTheirAnchor(t *testing.T) {
	bases := ShareLinkBases()
	if len(bases) == 0 {
		t.Fatal("ShareLinkBases() is empty")
	}
	if !bases[0].RequiresPrivilege {
		t.Error("the first base is the default and is expected to be the TUN one")
	}
	for _, base := range bases {
		if base.ID == "" || base.Name == "" || base.Description == "" || base.TemplateID == "" {
			t.Errorf("base %+v is incomplete", base)
		}
		doc, err := skeleton(base.TemplateID)
		if err != nil {
			t.Errorf("base %s: %v", base.ID, err)
			continue
		}
		items := outbounds(t, doc)
		found := false
		for _, item := range items {
			if item["tag"] == shareLinkPlaceholderTag {
				found = true
			}
		}
		if !found {
			t.Errorf("base %s: starter %s has no %q outbound to replace",
				base.ID, base.TemplateID, shareLinkPlaceholderTag)
		}
		if got, _ := doc["route"].(map[string]any)["final"].(string); got != shareLinkPlaceholderTag {
			t.Errorf("base %s: starter %s routes to %q, want the placeholder %q",
				base.ID, base.TemplateID, got, shareLinkPlaceholderTag)
		}
	}
}

// TestGeneratedConfigurationsPassTheManagedValidator runs what the dialog would create through
// the real validator: a profile that the binary rejects is a profile that can never start
// (spec §37, §38). Skipped when no binary is installed.
func TestGeneratedConfigurationsPassTheManagedValidator(t *testing.T) {
	managed := managedBinaryForGeneratorTest(t)
	if managed == "" {
		t.Skip("no sing-box binary found; set SINGBOXUI_TEST_SINGBOX to validate the generated configurations")
	}
	pastes := []string{vlessLink, hysteria2Link, vlessLink + "\n" + hysteria2Link}
	for _, base := range ShareLinkBases() {
		for _, paste := range pastes {
			generated, err := GenerateFromShareLinks(ShareLinkInput{Links: paste, Base: base.ID})
			if err != nil {
				t.Fatalf("base %s: %v", base.ID, err)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(generated.ConfigJSON), 0o600); err != nil {
				t.Fatalf("writing the generated configuration: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			result, err := singbox.Check(ctx, managed, path, 25*time.Second)
			cancel()
			if err != nil || !result.OK {
				t.Errorf("base %s: the managed validator rejected a generated configuration: %v\n%s",
					base.ID, err, result.Output)
			}
		}
	}
}

// TestCreateFromShareLinksCreatesAProfileWithTheGeneratedRevision checks the use case end to
// end against the real store: a profile appears, its first revision is the generated document
// and the revision says where it came from.
func TestCreateFromShareLinksCreatesAProfileWithTheGeneratedRevision(t *testing.T) {
	h := newHarness(t)
	created, generated, err := h.svc.CreateFromShareLinks(context.Background(), ShareLinkInput{
		Links:       vlessLink,
		Description: "Из ссылки",
	})
	if err != nil {
		t.Fatalf("CreateFromShareLinks() failed: %v", err)
	}
	if created.Name != "Node One" {
		t.Errorf("name = %q, want the link's remark as the fallback", created.Name)
	}
	revisions := h.revisions(t, created.ID, 10)
	if len(revisions) != 1 {
		t.Fatalf("revisions = %d, want the first one", len(revisions))
	}
	if revisions[0].Source != profile.SourceShareImport {
		t.Errorf("revision source = %q, want %q", revisions[0].Source, profile.SourceShareImport)
	}
	if revisions[0].ConfigJSON != strings.TrimSpace(generated.ConfigJSON) {
		t.Error("the stored revision is not the generated configuration")
	}
	if revisions[0].StructuralValidation != profile.StatusPassed {
		t.Errorf("structural validation = %q, want passed", revisions[0].StructuralValidation)
	}
	if h.em.count(events.ProfilesChanged) == 0 {
		t.Error("no profiles:changed event was emitted")
	}
}

func TestCreateFromShareLinksLetsTheTypedNameWin(t *testing.T) {
	h := newHarness(t)
	created, _, err := h.svc.CreateFromShareLinks(context.Background(), ShareLinkInput{
		Name:  "Мой сервер",
		Links: hysteria2Link,
	})
	if err != nil {
		t.Fatalf("CreateFromShareLinks() failed: %v", err)
	}
	if created.Name != "Мой сервер" {
		t.Errorf("name = %q, want the name the user typed", created.Name)
	}
}

func TestCreateFromShareLinksCreatesNothingForUnusableLinks(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.svc.CreateFromShareLinks(context.Background(), ShareLinkInput{
		Links: "https://example.com/not-a-link",
	}); err == nil {
		t.Fatal("CreateFromShareLinks() succeeded, want an error")
	}
	items, err := h.svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("profiles = %d, want none: a refused paste must not leave a profile behind", len(items))
	}
}

// ------------------------------------------------------------------ helpers

func hasInboundOfType(doc map[string]any, kind string) bool {
	for _, item := range asAnySlice(doc["inbounds"]) {
		if object, ok := item.(map[string]any); ok && object["type"] == kind {
			return true
		}
	}
	return false
}

func hasRuleRoutingPrivateAddressesDirectly(doc map[string]any) bool {
	route, ok := doc["route"].(map[string]any)
	if !ok {
		return false
	}
	for _, item := range asAnySlice(route["rules"]) {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if rule["ip_is_private"] == true && rule["outbound"] == "direct" {
			return true
		}
	}
	return false
}

// managedBinaryForGeneratorTest returns an installed sing-box for the validator-backed test:
// SINGBOXUI_TEST_SINGBOX wins, then the managed install directory, then PATH. An empty result
// skips the test instead of failing it.
func managedBinaryForGeneratorTest(t *testing.T) string {
	t.Helper()
	if explicit := strings.TrimSpace(os.Getenv("SINGBOXUI_TEST_SINGBOX")); explicit != "" {
		if !singbox.IsExecutable(explicit) {
			t.Fatalf("SINGBOXUI_TEST_SINGBOX=%s is not an executable file", explicit)
		}
		return explicit
	}
	if plat, err := platform.New(); err == nil {
		matches, _ := filepath.Glob(filepath.Join(plat.Paths().BinDir, "*", singbox.ExecutableName(runtime.GOOS)))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		for _, candidate := range matches {
			if singbox.IsExecutable(candidate) {
				return candidate
			}
		}
	}
	if found, err := exec.LookPath("sing-box"); err == nil {
		return found
	}
	return ""
}
