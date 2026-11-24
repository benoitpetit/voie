package providers

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// CapturePerplexityCookie opens a visible Chromium window on Perplexity and
// waits for the user to sign in, then returns the session cookie. Only the
// cookie name is logged; its value never appears in logs.
func CapturePerplexityCookie(parent context.Context) (string, error) {
	executable, err := findDuckAIBrowser()
	if err != nil {
		return "", fmt.Errorf("no Chromium-based browser found for Perplexity login: %w", err)
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.ExecPath(executable),
		chromedp.Flag("headless", false),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(parent, options...)
	defer cancelAllocator()
	ctx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()
	ctx, cancelTimeout := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelTimeout()
	if err := chromedp.Run(ctx, chromedp.Navigate("https://www.perplexity.ai/")); err != nil {
		return "", fmt.Errorf("open Perplexity login page: %w", err)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var cookies []*network.Cookie
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(actionCtx context.Context) error {
			var err error
			cookies, err = network.GetCookies().WithURLs([]string{"https://www.perplexity.ai/"}).Do(actionCtx)
			return err
		}))
		if err == nil {
			for _, cookie := range cookies {
				if cookie != nil && cookie.Name == "__Secure-next-auth.session-token" && cookie.Value != "" {
					return cookie.Value, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for Perplexity sign-in; run `voie connect pplx-proxy` in a terminal with a browser")
		case <-ticker.C:
		}
	}
}
