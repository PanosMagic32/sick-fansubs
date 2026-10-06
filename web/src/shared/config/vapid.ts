/**
 * VAPID public key — the
 * `applicationServerKey` the browser passes to `PushManager.subscribe`.
 *
 * PUBLIC material, not a secret: the private half lives server-side as the
 * compose secret `vapid_private_key`. The value below is the
 * PRODUCTION pair's public half; the rotation flow is: regenerate both
 * halves, swap every copy, let users resubscribe.
 *
 * The Go config default (internal/config/config.go `defaultVAPIDPublicKey`),
 * `.env.example`, and the compose `VAPID_PUBLIC_KEY:-…` fallback are the SAME
 * value, pinned equal by `TestVAPIDPublicKeyMatchesFrontend`,
 * `TestVAPIDPublicKeyMatchesEnvExample`, and
 * `TestVAPIDPublicKeyMatchesComposeFallback` (internal/config/vapid_pin_test.go)
 * — drift fails a test instead of shipping a keypair mismatch that the push
 * services would reject. The startup pair check
 * (`config.ValidateVAPIDPair`) additionally proves the private half on the
 * server belongs to this public key.
 */
export const VAPID_PUBLIC_KEY =
  "BHTZD8cRI0vmAwkO9fcsY7KwL2vL7vx1OByBARaSItTq2kqQNiPqLJa7zHSIgj8yRlAxGgO5ZAvxwMyMeNqafdc";
