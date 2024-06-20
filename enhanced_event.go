package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip10"
	"github.com/nbd-wtf/go-nostr/nip19"
	sdk "github.com/nbd-wtf/nostr-sdk"
	"github.com/texttheater/golang-levenshtein/levenshtein"
)

type EnhancedEvent struct {
	*nostr.Event
	relays  []string
	subject string
	summary string
	author  sdk.ProfileMetadata
}

func NewEnhancedEvent(
	ctx context.Context,
	event *nostr.Event,
	relays []string,
) EnhancedEvent {
	ee := EnhancedEvent{
		Event:  event,
		relays: relays,
	}

	for _, tag := range event.Tags {
		if tag[0] == "subject" || tag[0] == "title" {
			ee.subject = tag[1]
		}
		if tag[0] == "summary" {
			ee.summary = tag[1]
		}
	}

	if ctx != nil {
		if event.Kind == 0 {
			spm, _ := sdk.ParseMetadata(event)
			ee.author = spm
		} else {
			ctx, cancel := context.WithTimeout(ctx, time.Second*3)
			defer cancel()
			ee.author = sys.FetchProfileMetadata(ctx, event.PubKey)
		}
	}

	return ee
}

func (ee EnhancedEvent) authorLong() string {
	if ee.author.Name != "" {
		return fmt.Sprintf("%s (%s)", ee.author.Name, ee.author.NpubShort())
	}
	return ee.author.Npub()
}

func (ee EnhancedEvent) getParentNevent() string {
	parentNevent := ""
	switch ee.Kind {
	case 1, 1063:
		replyTag := nip10.GetImmediateReply(ee.Tags)
		if replyTag != nil {
			var relays []string
			if (len(*replyTag) > 2) && ((*replyTag)[2] != "") {
				relays = []string{(*replyTag)[2]}
			}
			eventId := (*replyTag)[1]
			if (*replyTag)[0] == "a" { // Reply to a ndaddr event
				eventId = strings.Split(eventId, ":")[1]
				kind, _ := strconv.Atoi(strings.Split((*replyTag)[1], ":")[0])
				identifier := strings.Split((*replyTag)[1], ":")[2]
				var relays []string
				if (len(*replyTag) > 2) && ((*replyTag)[2] != "") {
					relays = []string{(*replyTag)[2]}
				}
				parentNevent, _ = nip19.EncodeEntity(
					eventId,
					kind,
					identifier,
					relays)
			} else {
				parentNevent, _ = nip19.EncodeEvent(eventId, relays, "")
			}
		}
	case 1311:
		if atag := ee.Tags.GetFirst([]string{"a", ""}); atag != nil {
			parts := strings.Split((*atag)[1], ":")
			kind, _ := strconv.Atoi(parts[0])
			var relays []string
			if (len(*atag) > 2) && ((*atag)[2] != "") {
				relays = []string{(*atag)[2]}
			}
			parentNevent, _ = nip19.EncodeEntity(parts[1], kind, parts[2], relays)
		}
	}

	return parentNevent
}

func (ee EnhancedEvent) isReply() bool {
	return nip10.GetImmediateReply(ee.Event.Tags) != nil
}

func (ee EnhancedEvent) Preview() template.HTML {
	lines := strings.Split(html.EscapeString(ee.Event.Content), "\n")
	var processedLines []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		processedLine := shortenNostrURLs(line)
		processedLines = append(processedLines, processedLine)
	}

	return template.HTML(strings.Join(processedLines, "<br/>"))
}

func (ee EnhancedEvent) RssTitle() string {
	regex := regexp.MustCompile(`(?i)<br\s?/?>`)
	replacedString := regex.ReplaceAllString(string(ee.Preview()), " ")
	words := strings.Fields(replacedString)
	title := ""
	for i, word := range words {
		if len(title)+len(word)+1 <= 65 { // +1 for space
			if title != "" {
				title += " "
			}
			title += word
		} else {
			if i > 1 { // the first word len is > 65
				title = title + " ..."
			} else {
				title = ""
			}
			break
		}
	}

	content := ee.RssContent()
	distance := levenshtein.DistanceForStrings([]rune(title), []rune(content), levenshtein.DefaultOptions)
	similarityThreshold := 5
	if distance <= similarityThreshold {
		return ""
	} else {
		return title
	}
}

func (ee EnhancedEvent) RssContent() string {
	content := ee.Event.Content
	content = basicFormatting(html.EscapeString(content), true, false, false)
	content = renderQuotesAsHTML(context.Background(), content, false)
	if nevent := ee.getParentNevent(); nevent != "" {
		neventShort := nevent[:8] + "…" + nevent[len(nevent)-4:]
		content = "In reply to <a href='/" + nevent + "'>" + neventShort + "</a><br/>_________________________<br/><br/>" + content
	}
	return content
}

func (ee EnhancedEvent) Thumb() string {
	imgRegex := regexp.MustCompile(`(https?://[^\s]+\.(?:png|jpe?g|gif|bmp|svg)(?:/[^\s]*)?)`)
	matches := imgRegex.FindAllStringSubmatch(ee.Event.Content, -1)
	if len(matches) > 0 {
		// The first match group captures the image URL
		return matches[0][1]
	}
	return ""
}

func (ee EnhancedEvent) Npub() string {
	npub, _ := nip19.EncodePublicKey(ee.Event.PubKey)
	return npub
}

func (ee EnhancedEvent) NpubShort() string {
	npub := ee.Npub()
	return npub[:8] + "…" + npub[len(npub)-4:]
}

func (ee EnhancedEvent) Nevent() string {
	nevent, _ := nip19.EncodeEvent(ee.Event.ID, ee.relays, ee.Event.PubKey)
	return nevent
}

func (ee EnhancedEvent) CreatedAtStr() string {
	return time.Unix(int64(ee.Event.CreatedAt), 0).Format("2006-01-02 15:04:05")
}

func (ee EnhancedEvent) ModifiedAtStr() string {
	return time.Unix(int64(ee.Event.CreatedAt), 0).Format("2006-01-02T15:04:05Z07:00")
}

func (ee EnhancedEvent) ToJSONHTML() template.HTML {
	tagsHTML := "["
	for t, tag := range ee.Tags {
		tagsHTML += "\n    ["
		for i, item := range tag {
			cls := `"text-zinc-500 dark:text-zinc-50"`
			if i == 0 {
				cls = `"text-amber-500 dark:text-amber-200"`
			}
			itemJSON, _ := json.Marshal(item)
			tagsHTML += "\n      <span class=" + cls + ">" + html.EscapeString(string(itemJSON))
			if i < len(tag)-1 {
				tagsHTML += ","
			} else {
				tagsHTML += "\n    "
			}
		}
		tagsHTML += "]"
		if t < len(ee.Tags)-1 {
			tagsHTML += ","
		} else {
			tagsHTML += "\n  "
		}
	}
	tagsHTML += "]"

	contentJSON, _ := json.Marshal(ee.Content)

	keyCls := "text-purple-700 dark:text-purple-300"

	return template.HTML(fmt.Sprintf(
		`{
  <span class="`+keyCls+`">"id":</span> <span class="text-zinc-500 dark:text-zinc-50">"%s"</span>,
  <span class="`+keyCls+`">"pubkey":</span> <span class="text-zinc-500 dark:text-zinc-50">"%s"</span>,
  <span class="`+keyCls+`">"created_at":</span> <span class="text-green-600">%d</span>,
  <span class="`+keyCls+`">"kind":</span> <span class="text-amber-500 dark:text-amber-200">%d</span>,
  <span class="`+keyCls+`">"tags":</span> %s,
  <span class="`+keyCls+`">"content":</span> <span class="text-zinc-500 dark:text-zinc-50">%s</span>,
  <span class="`+keyCls+`">"sig":</span> <span class="text-zinc-500 dark:text-zinc-50 content">"%s"</span>
}`, ee.ID, ee.PubKey, ee.CreatedAt, ee.Kind, tagsHTML, html.EscapeString(string(contentJSON)), ee.Sig),
	)
}
