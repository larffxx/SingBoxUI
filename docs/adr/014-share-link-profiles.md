# ADR 014 — A share link that becomes a whole profile

## Context

The legacy application imported a share link into the one configuration it had
(`POST /api/share/import` → the parsed outbound appended, `docs/architecture/current-state.md`), and
the feature matrix carries that row as **IMPROVE**. What the new application ships today parses links
and pastes the parsed outbounds into the outbounds tab of a profile that already exists
(`ShareAPI.ParseShareLink` → `SharePasteDialog` → a new revision). That is the right contract for
editing, and it is not what a user does when they arrive: the first thing they have is a link from a
provider, and what they want from it is a *profile*.

The gap is not parsing — `internal/app/share` reads `vless`, `vmess`, `trojan`, `ss`, `hysteria2` and
`tuic` links and produces sing-box outbound objects, with table-driven tests and a validator-backed
regression suite. The gap is that a link describes **one outbound**, while a profile is a document
that also needs inbounds, DNS and routing:

* without an inbound the profile listens on nothing, so it cannot be started or even meaningfully
  tested;
* without `route.final` (or a rule) the parsed outbound carries no traffic;
* a TUN profile needs the DNS resolution of the proxied server to go through the proxy, or the
  first connection fails on a name the core cannot resolve.

The application already carries that document: the starters of `internal/app/templates`. Two of them
are exactly the shapes a link-generated profile needs, and both route through a placeholder outbound
tagged `proxy`:

* `tun-vless-reality` — TUN inbound, mixed-port fallback, DNS with the proxied resolver, private
  addresses routed directly, `route.final: proxy`;
* `socks-local` — mixed port only, no TUN, the same `proxy` placeholder.

`profile.SourceShareImport` has existed in the domain since the profile model was written and was
used by nothing: the flow this ADR describes is the one it was reserved for.

## Decision

A pasted share link **generates a complete configuration**, and the profile is created from it.

* **Generated, not assembled by the user.** `profiles.GenerateFromShareLinks` decodes the skeleton of
  the chosen base, deletes the placeholder outbound and puts the parsed ones in its place, retargets
  `route.final`, the DNS `detour` and any rule that named the placeholder to the outbound that now
  carries the traffic, and encodes the result. Everything else in the skeleton — the inbounds, the DNS
  servers, the rules that keep private addresses local, `auto_detect_interface` — is left as the
  starter wrote it.
* **One link, or several.** A single link becomes the route's final outbound, so the generated
  profile names its server directly. Several links become a `urltest` group that leads the outbounds
  (`auto`, renamed if a link already took the tag), because a profile that holds twenty servers and
  cannot pick among them is not usable without further editing. The group is the shape the selector
  starter already uses.
* **The starters are the skeletons.** The bases are declared by the backend (`ShareLinkBases`: the TUN
  one first, because the product is a VPN, and the local one for a server that is only needed by a
  program or a port) and each names the starter it is built on. There is no second copy of the
  documents: editing a starter changes what a link generates, and a test pins the anchor — every
  declared base must name a starter that routes through the `proxy` placeholder — so the coupling
  cannot rot silently.
* **The document is validated like any other.** `CreateFromShareLinks` creates the profile through
  `profiles.Import`, which means the structural validation runs, the revision records
  `SourceShareImport`, the failure path creates nothing at all, and the validator-backed tests run the
  generated configurations through the shipped `sing-box` binary.
* **Unusable input is reported, not swallowed.** A paste in which one line of several is unusable
  still generates, and the unusable lines come back as warnings carrying their source line
  (`share.FailureMessage` adds the details `MessageOf` drops) — the same split the paste dialog
  already made. A paste with nothing usable is refused with `SHARE_LINK_INVALID` and no profile.
* **Tags never collide.** Parsed tags are uniquified against every tag the skeleton already uses, so a
  link whose remark is `direct` cannot take the place of the starter's fallback outbound.
* **The name falls back to the link.** The remarks of the first link name the profile when the user
  typed no name; the dialog shows that name while they type.

## Alternatives

* **Generate in the frontend.** Rejected for the reason ADR 013 established: the frontend renders what
  the backend declares. The skeleton, the tag rules and the retargeting are decisions about a sing-box
  document, and a second implementation in TypeScript would be a second source of truth.
* **Keep the skeleton copy inside the generator.** Two documents that must stay valid, two places to
  update when sing-box removes a field (1.12 removed the legacy DNS server form, 1.13 the legacy
  inbound fields, 1.14 the fallback domain resolver). Rejected: the starters are already maintained,
  validated against the real binary by their own tests, and shown to the user.
* **Parse only, create an empty profile with the outbound in it.** The profile would listen on
  nothing and route nothing: the user would still have to build the inbound, DNS and routing by hand,
  which is the work this feature exists to remove.
* **Always route through a group, even for one link.** A group of one hides which server the profile
  uses and adds a moving part (the group's own health checks) for no benefit. One link is one
  outbound.
* **Extend `SharePasteDialog` instead of adding a create-time dialog.** The paste dialog's contract is
  "here are outbounds to insert into the document you are editing" — it returns `outbounds` and
  `notes`, and it never creates anything. Creating a profile has a different contract (a profile, its
  base, its name, its warnings) and a different failure mode (a name conflict), so it is its own
  dialog and its own API call.
* **A subscription URL instead of links.** Fetching a provider's subscription is a network feature
  with its own update story (refresh, cache, failure), and the request was about pasting a link. Not
  in this change.

## Consequences

* A profile created from a link is the starter the user would have picked, with the server filled in —
  the same inbounds, DNS and routing, and the same appearance in the raw editor.
* The generated document is a plain `map[string]any` marshalled with sorted keys, so the JSON the user
  sees is stable and diffable, but it is not in the starters' hand-written key order. The revision
  editor reformats it the moment the user touches it; nothing depends on the order.
* The parse produces warnings that are shown twice: before creating, from the preview
  (`ShareAPI.ParseShareLinks`), and after, from the created payload. That is deliberate — the preview
  answers "what did it understand", the payload answers "what was left out of the document".
* Multi-link profiles depend on the `urltest` group's own probing (every five minutes, the interval
  the selector starter uses). A user who wants a fixed server edits the group, exactly as they would
  in a hand-built profile.
* `SHARE_LINK_INVALID` and `PROFILE_NAME_CONFLICT` are the failures the dialog has to explain, and
  both already carry a hint in the frontend's error map.
* The share-link flow exercises the validator on every creation, which is another reason the
  windowless-helper decision of ADR 015 matters: creating a profile is one paste and two helper
  processes.
* Verified on Windows 11 with the managed sing-box 1.14.0: both bases, with one and with two links,
  pass `sing-box check`; the TUN base generates a `tun` inbound and the local base does not; a link
  whose remark collides with the starter's `direct` outbound is renamed (`direct-2`) and the route
  follows it; an unusable line leaves no profile behind.
