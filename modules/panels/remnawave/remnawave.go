// Package remnawave connects subscriptions to a Remnawave 3.x panel through
// the official Go SDK (github.com/Jolymmiles/remnawave-api-go/v3).
package remnawave

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	remapi "github.com/Jolymmiles/remnawave-api-go/v3/api"
	"github.com/google/uuid"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/panels"
)

func init() { tors.RegisterModule(new(Panel)) }

// Panel is a Remnawave panel.
type Panel struct {
	// URL of the panel, e.g. https://panel.example.com.
	URL string `json:"url"`
	// Token is an API token from the panel settings.
	Token string `json:"token"`
	// Squads are internal squad UUIDs for plans that name none.
	Squads []string `json:"squads,omitzero"`
	// Prefix starts the usernames of accounts the bot creates.
	Prefix string `json:"prefix,omitzero"`

	api *remapi.ClientExt
}

func (*Panel) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "panels.providers.remnawave", New: func() tors.Module { return new(Panel) }}
}

func (*Panel) Market() market.Info {
	return market.Info{
		Name:     "Remnawave",
		Summary:  "Создаёт и продлевает подписки в панели Remnawave 3.x.",
		Category: "panel",
		Requires: []string{"panels"},
		Host:     &market.Host{App: "panels", Field: "providers", Key: "provider", Named: true},
		Config: []market.Field{
			{Key: "url", Title: "Адрес панели", Type: "text", Placeholder: "https://panel.example.com", Required: true},
			{Key: "token", Title: "API-токен", Type: "secret", Default: "{env.REMNAWAVE_TOKEN}", Required: true,
				Hint: "Создаётся в панели: Настройки → API-токены"},
			{Key: "squads", Title: "Внутренние сквады", Type: "strings",
				Hint: "UUID сквадов для тарифов, у которых они не указаны"},
		},
	}
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,20}$`)

func (p *Panel) Provision(tors.Context) error {
	if p.URL == "" || p.Token == "" {
		return errors.New("нужны url и token панели")
	}
	if p.Prefix == "" {
		p.Prefix = "tors"
	}
	if !usernameRe.MatchString(p.Prefix) {
		return fmt.Errorf("prefix %q: только латиница, цифры, _ и -, до 20 символов", p.Prefix)
	}
	for _, s := range p.Squads {
		if _, err := uuid.Parse(s); err != nil {
			return fmt.Errorf("squads: %q — не UUID", s)
		}
	}
	c, err := remapi.NewClient(strings.TrimRight(p.URL, "/"), remapi.StaticToken{Token: p.Token},
		remapi.WithClient(&http.Client{Timeout: 20 * time.Second}))
	if err != nil {
		return err
	}
	p.api = remapi.NewClientExt(c)
	return nil
}

func (p *Panel) Ensure(ctx context.Context, ref string, a panels.Account) (panels.Access, error) {
	squads, err := p.squads(a.Targets)
	if err != nil {
		return panels.Access{}, err
	}
	name := p.Prefix + "_" + a.Name
	if ref == "" {
		// A retry after a crash may find the account already created.
		res, err := p.api.Users().GetUserByUsername(ctx, name)
		if err != nil {
			return panels.Access{}, err
		}
		if u, err := user(res); err == nil {
			ref = strconv.Itoa(u.ID)
		} else if _, missing := res.(*remapi.NotFoundError); !missing {
			return panels.Access{}, err
		}
	}
	if ref == "" {
		body := &remapi.CreateUserBody{
			Username:             name,
			Status:               remapi.NewOptCreateUserBodyStatus(remapi.CreateUserBodyStatusACTIVE),
			ExpireAt:             a.ExpiresAt.UTC(),
			TrafficLimitBytes:    remapi.NewOptInt(a.TrafficGB << 30),
			ActiveInternalSquads: squads,
			Description:          remapi.NewOptString(a.Description),
		}
		if a.TelegramID != 0 {
			body.TelegramId = remapi.NewOptNilInt(int(a.TelegramID))
		}
		if a.Devices > 0 {
			body.HwidDeviceLimit = remapi.NewOptInt(a.Devices)
		}
		res, err := p.api.Users().CreateUser(ctx, body)
		if err != nil {
			return panels.Access{}, err
		}
		return access(res)
	}
	id, err := strconv.Atoi(ref)
	if err != nil {
		return panels.Access{}, fmt.Errorf("ref %q: %w", ref, err)
	}
	body := &remapi.UpdateUserBody{
		ID:                   remapi.NewOptInt(id),
		ExpireAt:             remapi.NewOptDateTime(a.ExpiresAt.UTC()),
		TrafficLimitBytes:    remapi.NewOptInt(a.TrafficGB << 30),
		ActiveInternalSquads: squads,
		Description:          remapi.NewOptNilString(a.Description),
	}
	if a.ExpiresAt.After(time.Now()) {
		body.Status = remapi.NewOptUpdateUserBodyStatus(remapi.UpdateUserBodyStatusACTIVE)
	}
	if a.Devices > 0 {
		body.HwidDeviceLimit = remapi.NewOptNilInt(a.Devices)
	}
	res, err := p.api.Users().UpdateUser(ctx, body)
	if err != nil {
		return panels.Access{}, err
	}
	return access(res)
}

func (p *Panel) Revoke(ctx context.Context, ref string) (panels.Access, error) {
	id, err := strconv.Atoi(ref)
	if err != nil {
		return panels.Access{}, fmt.Errorf("ref %q: %w", ref, err)
	}
	res, err := p.api.Users().RevokeUserSubscription(ctx, &remapi.RevokeUserSubscriptionBody{}, id)
	if err != nil {
		return panels.Access{}, err
	}
	return access(res)
}

func (p *Panel) Usage(ctx context.Context, ref string) (panels.Usage, error) {
	id, err := strconv.Atoi(ref)
	if err != nil {
		return panels.Usage{}, fmt.Errorf("ref %q: %w", ref, err)
	}
	res, err := p.api.Users().GetUserById(ctx, id)
	if err != nil {
		return panels.Usage{}, err
	}
	u, err := user(res)
	if err != nil {
		return panels.Usage{}, err
	}
	return panels.Usage{UsedBytes: int64(u.UserTraffic.UsedTrafficBytes)}, nil
}

func (p *Panel) Delete(ctx context.Context, ref string) error {
	id, err := strconv.Atoi(ref)
	if err != nil {
		return fmt.Errorf("ref %q: %w", ref, err)
	}
	res, err := p.api.Users().DeleteUser(ctx, id)
	if err != nil {
		return err
	}
	switch r := res.(type) {
	case *remapi.UsersDeleteUserNoContent, *remapi.NotFoundError:
		return nil
	default:
		return apiError(r)
	}
}

func (p *Panel) squads(targets []string) ([]uuid.UUID, error) {
	if len(targets) == 0 {
		targets = p.Squads
	}
	out := make([]uuid.UUID, 0, len(targets))
	for _, t := range targets {
		id, err := uuid.Parse(t)
		if err != nil {
			return nil, fmt.Errorf("сквад %q — не UUID", t)
		}
		out = append(out, id)
	}
	return out, nil
}

func user(res any) (remapi.UserItemInfo, error) {
	if r, ok := res.(*remapi.UserResponse); ok {
		return r.Response, nil
	}
	return remapi.UserItemInfo{}, apiError(res)
}

func access(res any) (panels.Access, error) {
	u, err := user(res)
	if err != nil {
		return panels.Access{}, err
	}
	return panels.Access{Ref: strconv.Itoa(u.ID), URL: u.SubscriptionUrl}, nil
}

func apiError(res any) error {
	switch r := res.(type) {
	case *remapi.BadRequestError:
		return fmt.Errorf("remnawave: %s", r.Message)
	case *remapi.NotFoundError:
		return fmt.Errorf("remnawave: не найдено: %s", r.Message)
	case *remapi.InternalServerError:
		return fmt.Errorf("remnawave: ошибка панели: %s", r.Message.Or("без описания"))
	}
	return fmt.Errorf("remnawave: неожиданный ответ %T", res)
}
