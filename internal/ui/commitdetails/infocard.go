package commitdetails

import (
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/icons"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	cardPadX        = 16
	cardPadY        = 14
	cardHeadingSize = 12.5
	cardHeadingStep = 24
	cardBodySize    = 9.5
	cardTextHalf    = 6
	cardChipHeight  = 20
	cardChipPadX    = 8
	cardChipGap     = 6
	cardChipIcon    = 12
	cardChipIconGap = 5
	cardChipTint    = 14
	cardSectionGap  = 8
	cardRowHeight   = 28
	cardRowIcon     = 15
	cardLabelGap    = 10
	cardLabelWidth  = 124
	cardAvatar      = 20
	cardAvatarGap   = 6
	cardDividerGap  = 10
	cardDividerH    = 14
	cardActionIcon  = 14
	cardActionGap   = 8
	cardHitPad      = 4
	cardBodyStep    = 15
)

type cardPalette struct {
	text    color.RGBA
	muted   color.RGBA
	accent  color.RGBA
	chip    color.RGBA
	border  color.RGBA
	surface color.RGBA
}

type cardHit struct {
	rect   image.Rectangle
	action func()
}

type infoCard struct {
	widget.Base

	mu      sync.Mutex
	details Details
	pal     cardPalette
	hits    []cardHit

	OnCopy   func(text string)
	OnParent func(id hash.ObjectID)
}

func newInfoCard() *infoCard {
	c := &infoCard{}
	c.Restyle(widget.CurrentTheme())
	return c
}

func (c *infoCard) Restyle(t *widget.Theme) {
	p := style.Of(t)
	c.mu.Lock()
	c.pal = cardPalette{
		text:    p.Text,
		muted:   p.Secondary,
		accent:  p.Accent,
		chip:    p.Tint(cardChipTint),
		border:  p.Border,
		surface: p.Field,
	}
	c.mu.Unlock()
	c.Invalidate()
}

func (c *infoCard) Show(details Details) {
	c.mu.Lock()
	c.details = details
	c.mu.Unlock()
	c.Invalidate()
}

func (c *infoCard) Text() string {
	c.mu.Lock()
	details := c.details
	c.mu.Unlock()
	if details.Commit.IsZero() {
		return i18n.T("Details.Empty")
	}
	return infoText(details)
}

func (c *infoCard) Draw(ctx widget.DrawContext) {
	c.mu.Lock()
	details, pal := c.details, c.pal
	c.mu.Unlock()
	b := c.Bounds()
	prev := ctx.Clip()
	ctx.SetClip(b.Intersect(prev))
	ctx.FillRect(b.Min.X, b.Min.Y, b.Dx(), b.Dy(), pal.surface)
	pen := &cardPen{ctx: ctx, pal: pal, x: b.Min.X + cardPadX, y: b.Min.Y + cardPadY, right: b.Max.X - cardPadX}
	if details.Commit.IsZero() {
		ctx.DrawTextSize(i18n.T("Details.Empty"), pen.x, pen.y, cardBodySize, pal.muted)
	} else {
		c.drawDetails(pen, details)
	}
	ctx.SetClip(prev)
	c.mu.Lock()
	c.hits = pen.hits
	c.mu.Unlock()
}

func (c *infoCard) drawDetails(pen *cardPen, details Details) {
	subject, body := splitMessage(details.Message)
	pen.ctx.DrawTextFont(subject, pen.x, pen.y, cardHeadingSize, widget.BuiltinFontBold, pen.pal.text)
	pen.y += cardHeadingStep
	pen.chips(details)
	pen.separator()

	commit := details.Commit.String()
	pen.row("file_text", i18n.T("Details.Label.Commit"), func(x, cy int) {
		end := pen.text(commit, x, cy, widget.BuiltinFontMono, pen.pal.text)
		pen.action("copy", end+cardActionGap, cy, func() { c.copy(commit) })
	})
	pen.row("person", i18n.T("Details.Label.Author"), func(x, cy int) {
		pen.person(x, cy, details.Author, details.AuthorAt, true)
	})
	pen.row("pencil", i18n.T("Details.Label.Committer"), func(x, cy int) {
		pen.person(x, cy, details.Committer, details.CommitAt, false)
	})
	if len(details.Parents) > 0 {
		pen.row("", i18n.T("Details.Label.Parents"), func(x, cy int) {
			for _, parent := range details.Parents {
				x = pen.parent(x, cy, parent, func() { c.openParent(parent) })
			}
		})
	}
	pen.body(body)
}

func (c *infoCard) copy(text string) {
	if c.OnCopy != nil {
		c.OnCopy(text)
	}
}

func (c *infoCard) openParent(id hash.ObjectID) {
	if c.OnParent != nil {
		c.OnParent(id)
	}
}

func (c *infoCard) hitAt(x, y int) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	pt := image.Pt(x, y)
	for _, hit := range c.hits {
		if pt.In(hit.rect) {
			return hit.action
		}
	}
	return nil
}

func (c *infoCard) OnMouseButton(e widget.MouseEvent) bool {
	if e.Button != widget.MouseLeft || !e.Pressed {
		return false
	}
	action := c.hitAt(e.X, e.Y)
	if action == nil {
		return false
	}
	action()
	return true
}

func (c *infoCard) Cursor(x, y int) widget.Cursor {
	if c.hitAt(x, y) != nil {
		return widget.CursorHand
	}
	return widget.CursorArrow
}

type cardPen struct {
	ctx   widget.DrawContext
	pal   cardPalette
	x     int
	y     int
	right int
	hits  []cardHit
}

func (p *cardPen) text(text string, x, cy int, font string, col color.RGBA) int {
	p.ctx.DrawTextFont(text, x, cy-cardTextHalf, cardBodySize, font, col)
	return x + p.ctx.MeasureTextFont(text, cardBodySize, font)
}

func (p *cardPen) chips(details Details) {
	x := p.x
	for _, branch := range details.Branches {
		x += p.chip(x, p.y, icons.TreeTinted("branch", cardChipIcon, p.pal.accent), branch, "") + cardChipGap
	}
	for _, tag := range details.Tags {
		x += p.chip(x, p.y, icons.TreeTinted("tag", cardChipIcon, p.pal.accent), tag, "") + cardChipGap
	}
	if x != p.x {
		p.y += cardChipHeight + cardSectionGap
	}
}

func (p *cardPen) chip(x, y int, icon image.Image, text, font string) int {
	width := cardChipPadX*2 + p.ctx.MeasureTextFont(text, cardBodySize, font)
	if icon != nil {
		width += cardChipIcon + cardChipIconGap
	}
	p.ctx.FillRoundRect(x, y, width, cardChipHeight, cardChipHeight/2, p.pal.chip)
	textX := x + cardChipPadX
	if icon != nil {
		p.ctx.DrawImageScaled(icon, textX, y+(cardChipHeight-cardChipIcon)/2, cardChipIcon, cardChipIcon)
		textX += cardChipIcon + cardChipIconGap
	}
	p.ctx.DrawTextFont(text, textX, y+cardChipHeight/2-cardTextHalf, cardBodySize, font, p.pal.accent)
	return width
}

func (p *cardPen) separator() {
	p.ctx.DrawHLine(p.x, p.y, p.right-p.x, p.pal.border)
	p.y++
}

func (p *cardPen) row(icon, label string, value func(x, cy int)) {
	cy := p.y + cardRowHeight/2
	name := icon
	if name == "" {
		p.ctx.DrawImageScaled(icons.TreeTinted("branch", cardRowIcon, p.pal.muted), p.x, cy-cardRowIcon/2, cardRowIcon, cardRowIcon)
	} else {
		p.ctx.DrawImageScaled(icons.Card(name, cardRowIcon, p.pal.muted), p.x, cy-cardRowIcon/2, cardRowIcon, cardRowIcon)
	}
	p.text(label, p.x+cardRowIcon+cardLabelGap, cy, "", p.pal.muted)
	value(p.x+cardLabelWidth, cy)
	p.y += cardRowHeight
	p.separator()
}

func (p *cardPen) person(x, cy int, name string, at time.Time, avatar bool) {
	if avatar {
		p.ctx.FillRoundRect(x, cy-cardAvatar/2, cardAvatar, cardAvatar, cardAvatar/2, p.pal.chip)
		if initial := initialOf(name); initial != "" {
			width := p.ctx.MeasureTextFont(initial, cardBodySize, widget.BuiltinFontBold)
			p.ctx.DrawTextFont(initial, x+(cardAvatar-width)/2, cy-cardTextHalf, cardBodySize, widget.BuiltinFontBold, p.pal.accent)
		}
		x += cardAvatar + cardAvatarGap
	}
	end := p.text(name, x, cy, "", p.pal.text)
	date := when(at)
	if date == "" {
		return
	}
	divider := end + cardDividerGap
	p.ctx.DrawVLine(divider, cy-cardDividerH/2, cardDividerH, p.pal.border)
	p.text(date, divider+cardDividerGap, cy, "", p.pal.muted)
}

func (p *cardPen) parent(x, cy int, id hash.ObjectID, open func()) int {
	short := id.String()[:shortLength]
	width := p.chip(x, cy-cardChipHeight/2, nil, short, widget.BuiltinFontMono)
	arrow := x + width + cardActionGap
	p.ctx.DrawImageScaled(icons.Card("arrow_right", cardActionIcon, p.pal.muted), arrow, cy-cardActionIcon/2, cardActionIcon, cardActionIcon)
	p.hits = append(p.hits, cardHit{
		rect:   image.Rect(x, cy-cardChipHeight/2, arrow+cardActionIcon+cardHitPad, cy+cardChipHeight/2),
		action: open,
	})
	return arrow + cardActionIcon + cardChipGap*2
}

func (p *cardPen) action(icon string, x, cy int, run func()) {
	p.ctx.DrawImageScaled(icons.Menu(icon, cardActionIcon, p.pal.muted), x, cy-cardActionIcon/2, cardActionIcon, cardActionIcon)
	p.hits = append(p.hits, cardHit{
		rect:   image.Rect(x-cardHitPad, cy-cardActionIcon/2-cardHitPad, x+cardActionIcon+cardHitPad, cy+cardActionIcon/2+cardHitPad),
		action: run,
	})
}

func (p *cardPen) body(lines []string) {
	if len(lines) == 0 {
		return
	}
	p.y += cardSectionGap
	for _, line := range lines {
		p.ctx.DrawTextSize(line, p.x, p.y, cardBodySize, p.pal.text)
		p.y += cardBodyStep
	}
}

func splitMessage(message string) (string, []string) {
	subject, rest, _ := strings.Cut(strings.TrimRight(message, "\n"), "\n")
	rest = strings.Trim(rest, "\n")
	if rest == "" {
		return subject, nil
	}
	return subject, strings.Split(rest, "\n")
}

func initialOf(name string) string {
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return ""
}
