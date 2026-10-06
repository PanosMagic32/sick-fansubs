/**
 * Greek message catalog for the Sick-Fansubs application.
 *
 * All user-facing Greek copy lives here — never scattered through components.
 * Keys are organized by domain: `ui` (shared labels), `nav` (navigation),
 * `auth` (authentication pages), `account` (account page), `problems`
 * (RFC 9457 problem types), and `violations` (field-level validation codes).
 *
 * Components import `el` directly and look up keys.
 */

/** Shared UI labels used across multiple components. */
const ui = {
  loading: "Φόρτωση…",
  signIn: "Σύνδεση",
  signOut: "Αποσύνδεση",
  username: "Όνομα χρήστη",
  email: "Email",
  password: "Κωδικός πρόσβασης",
  currentPassword: "Τρέχων κωδικός",
  newPassword: "Νέος κωδικός",
  confirmPassword: "Επιβεβαίωση κωδικού",
  siteName: "Sick-Fansubs",
  error: "Κάτι πήγε στραβά.",
  // Deliberate anglicisms (owner ruling): the Greek community uses the
  // English "Online"/"Offline" for service status. Components render these
  // keys inside lang="en" so assistive tech pronounces them correctly
  // (app-footer).
  statusOnline: "Online",
  statusOffline: "Offline",
  skipToContent: "Παράλειψη πλοήγησης",
  account: "Λογαριασμός",
  home: "Αρχική",
  notFound: "Η σελίδα δε βρέθηκε.",
  // The "404" heading is catalog copy, not a structural glyph (the © /
  // v-prefix / — glyphs are exempt).
  notFoundTitle: "404",
  comingSoon: "Αυτή η σελίδα θα προστεθεί σύντομα.",
  retry: "Δοκιμή ξανά",
  serverConnectionFailed: "Αποτυχία σύνδεσης με τον διακομιστή.",
  invalidServerResponse: "Ο διακομιστής επέστρεψε μη έγκυρη απάντηση.",
  toggleTheme: "Εναλλαγή θέματος",
  menu: "Μενού",
  sessions: "Συσκευές",
  // Pagination + card affordances: the shared pager-nav and content-card
  // pieces reuse one set of labels.
  pagerLabel: "Σελιδοποίηση αποτελεσμάτων",
  prevPage: "Προηγούμενη σελίδα",
  nextPage: "Επόμενη σελίδα",
  // The page-size dropdown's visible label inside its chip.
  pageSizeLabel: "Στοιχεία ανά σελίδα",
  // Icon-only card affordance (❯❯) aria-label.
  details: "Λεπτομέρειες",
  // Relative-time floor — under a minute renders this instead of the Intl
  // relative wording.
  justNow: "μόλις τώρα",
} as const;

/**
 * The pager's position line: the 1-based ordinal and the page count the
 * caller derived from the loaded row total and the active page size
 * (already floored at one — an empty list still displays one page).
 */
export function pagerPageOf(page: number, pages: number): string {
  return `Σελίδα ${page} από ${pages}`;
}

/** Home page copy. */
const nav = {
  // Deliberate anglicism — the community uses the English word for this
  // nav item (same policy as Tracker/Online/Offline; owner ruling: the
  // label stays "Projects"). Rendered inside lang="en".
  projects: "Projects",
  search: "Αναζήτηση",
  // The about page — Greek copy, not an anglicism (owner ruling: nav link
  // text is "Η ομάδα").
  about: "Η ομάδα",
  tracker: "Tracker",
  mainNavigation: "Κύρια πλοήγηση",
  // Screen-reader-only context appended to external links (e.g. Tracker).
  // Parentheses are copy, not punctuation — kept in the catalog so the
  // full rendered string lives in one place.
  opensInNewTab: "(ανοίγει σε νέα καρτέλα)",
} as const;

/** Authentication-specific copy. */
// Shared recovery consequence (owner-directed) — ONE
// source for the warning wherever it renders (register hint, email-change
// hint, unverified banner) so the wording can never drift. "μόνοι σας"
// keeps the copy honest: staff break-glass reset stays a path,
// but self-service recovery is gone with a wrong address.
const emailRecoveryConsequence =
  "με λάθος email δεν θα μπορείτε να ανακτήσετε μόνοι σας τον κωδικό σας";

const auth = {
  signInTitle: "Σύνδεση",
  signInButton: "Σύνδεση",
  signInPending: "Σύνδεση…",
  registerTitle: "Εγγραφή",
  registerButton: "Δημιουργία λογαριασμού",
  registerPending: "Δημιουργία…",
  // Registration field errors: the server answers per-field codes and
  // the form names the rule that produced each one. The generic violations
  // map stays the fallback for every unmapped field/code pair and for the
  // other forms.
  registerUsernameLength: "Το όνομα χρήστη πρέπει να έχει 1–32 χαρακτήρες.",
  registerUsernameFormat:
    "Το όνομα χρήστη δέχεται λατινικά γράμματα, αριθμούς και κενά ανάμεσά τους — χωρίς άλλα σύμβολα.",
  registerUsernameTaken: "Το όνομα χρήστη χρησιμοποιείται ήδη.",
  registerEmailFormat: "Μη έγκυρη διεύθυνση email.",
  registerEmailTaken: "Το email χρησιμοποιείται ήδη.",
  registerPasswordMin: "Ο κωδικός πρέπει να έχει τουλάχιστον 8 χαρακτήρες.",
  registerPasswordMax: "Ο κωδικός δεν μπορεί να υπερβαίνει τα 72 byte.",
  // Recovery warning under email-entry fields (register + account
  // email change) — the email is the forgot-password recovery
  // identifier: a wrong address locks the owner
  // out of self-service reset.
  emailRecoveryHint: `Βεβαιωθείτε ότι το email είναι σωστό — ${emailRecoveryConsequence}.`,
  identifierLabel: "Όνομα χρήστη ή email",
  alreadyHaveAccount: "Έχετε ήδη λογαριασμό;",
  noAccount: "Δεν έχετε λογαριασμό; Δημιουργήστε έναν.",
  signOutAll: "Αποσύνδεση από όλες τις συσκευές",
  changePassword: "Αλλαγή κωδικού",
  passwordChanged: "Ο κωδικός άλλαξε επιτυχώς. Συνδεθείτε ξανά.",
  passwordsDoNotMatch: "Οι κωδικοί δεν ταιριάζουν.",
  wrongCurrentPassword: "Ο τρέχων κωδικός δεν είναι σωστός.",
  // Forced change: shown on the account page while the reset flag is
  // set — gated endpoints 403 until the password changes.
  forcedChangeNotice: "Πρέπει να αλλάξετε τον κωδικό σας για να συνεχίσετε.",
  // Self-service reset: the forgot + reset pages and the
  // sign-in page's link/banner.
  forgotLink: "Ξεχάσατε τον κωδικό σας;",
  forgotTitle: "Επαναφορά κωδικού",
  forgotButton: "Αποστολή συνδέσμου",
  forgotPending: "Αποστολή…",
  // Enumeration-safe: shown whether or not the account exists.
  forgotSuccess:
    "Αν υπάρχει λογαριασμός με αυτά τα στοιχεία, θα λάβετε email με σύνδεσμο επαναφοράς. Ο σύνδεσμος ισχύει για 30 λεπτά.",
  resetTitle: "Νέος κωδικός",
  resetIntro: "Ορίστε τον νέο σας κωδικό πρόσβασης.",
  resetButton: "Αλλαγή κωδικού",
  resetPending: "Αλλαγή…",
  // Shown on the sign-in page after a completed reset (sessionStorage
  // transport — the page banner owns the message).
  resetSuccess: "Ο κωδικός σας άλλαξε επιτυχώς. Συνδεθείτε με τον νέο κωδικό.",
  resetTokenInvalid:
    "Ο σύνδεσμος επαναφοράς δεν είναι έγκυρος ή έχει λήξει. Ζητήστε νέο σύνδεσμο.",
  backToSignIn: "Επιστροφή στη σύνδεση",
  // Email verification: the /auth/verify page.
  verifyTitle: "Επιβεβαίωση email",
  verifyPending: "Επιβεβαίωση…",
  // Rendered on /sign-in (anonymous success) and on /account (signed-in
  // success) — present tense fits both arrival states.
  verifySuccess: "Το email σας επιβεβαιώθηκε.",
  verifyTokenInvalid:
    "Ο σύνδεσμος επιβεβαίωσης δεν είναι έγκυρος ή έχει λήξει. Συνδεθείτε για να ζητήσετε νέο σύνδεσμο.",
} as const;

/**
 * Account page — profile section copy.
 *
 * Role labels map the four CHECK-constrained role identifiers to Greek
 * display values; the role hierarchy names the roles, not their Greek copy.
 */
const account = {
  profileTitle: "Προφίλ",
  memberSince: "Μέλος από",
  roleLabel: "Ρόλος",
  roleUser: "Μέλος",
  roleModerator: "Συντονιστής",
  roleAdmin: "Διαχειριστής",
  roleSuperAdmin: "Ανώτατος διαχειριστής",
  // Defense-in-depth fallback: roles are CHECK-constrained, so an unknown
  // value can never persist — but the fallback stays Greek
  // copy rather than leaking the raw identifier.
  roleUnknown: "Άγνωστος ρόλος",
  // Account-section tab chrome (owner ruling): the page's sections are
  // tabs — profile (identity + devices), favorites, security (password
  // change).
  tabsLabel: "Ενότητες λογαριασμού",
  securityTab: "Ασφάλεια",
  // Notifications tab — the push settings + install
  // control section. Same word as el.titles.notifications on purpose (the
  // page title and the tab name one surface), but kept as a UI label in
  // this group rather than reaching into the titles group.
  notificationsTab: "Ειδοποιήσεις",
  // Deliberate anglicism — the community term (owner ruling), rendered
  // inside lang="en" and shown in parentheses after the label so the tab
  // says what the section is for.
  notificationsTabPush: "push",
  // Avatar upload — the profile section's self-service widget.
  avatarChoose: "Επιλογή εικόνας",
  avatarReplace: "Αλλαγή εικόνας",
  avatarUpload: "Ανέβασμα",
  avatarUploading: "Ανέβασμα…",
  avatarSuccess: "Η εικόνα προφίλ ενημερώθηκε.",
  avatarTooLarge: "Το αρχείο είναι πολύ μεγάλο.",
  avatarInvalid: "Μη έγκυρη μορφή εικόνας.",
  // Email verification — the account page's verified-status
  // display, the unverified banner, the resend button, and the
  // email-change widget.
  emailVerifiedLabel: "Επιβεβαιωμένο email",
  emailUnverifiedLabel: "Το email δεν έχει επιβεβαιωθεί.",
  emailUnverifiedHint: `Ελέγξτε τα εισερχόμενά σας για τον σύνδεσμο επιβεβαίωσης. Αν η διεύθυνση δεν είναι σωστή, αλλάξτε την — ${emailRecoveryConsequence}.`,
  resendVerification: "Νέα αποστολή συνδέσμου επιβεβαίωσης",
  resendVerificationPending: "Αποστολή…",
  resendSuccess: "Στάλθηκε νέο μήνυμα επιβεβαίωσης.",
  resendError: "Η αποστολή απέτυχε. Δοκιμάστε ξανά αργότερα.",
  changeEmailButton: "Αλλαγή email",
  changeEmailCancel: "Ακύρωση",
  changeEmailPending: "Αλλαγή…",
  // Deliberately neutral — the server may have failed the verification
  // send (the change still succeeds); the unverified banner
  // below covers the "check your inbox / resend" guidance honestly.
  changeEmailSuccess: "Το email άλλαξε.",
  // Post-registration notice on /account: honest — the server
  // may have failed the send, so the copy points at the resend
  // surface instead of promising a delivered email.
  registrationSuccess:
    "Ο λογαριασμός δημιουργήθηκε. Επιβεβαιώστε τη διεύθυνση email σας — αν δεν λάβατε σύνδεσμο, ζητήστε νέα αποστολή.",
  // Active sessions — the "Συσκευές" block of the profile
  // tab. The device label itself is data (a browser/platform product name
  // like "Chrome · Android"), never catalog copy; only the fallback for an
  // absent label is Greek. The heading is el.ui.sessions.
  sessionsCurrent: "Αυτή η συσκευή",
  sessionsUnknownDevice: "Άγνωστη συσκευή",
  sessionsStartedAt: "Σύνδεση",
  sessionsExpiresAt: "Λήξη",
  sessionsEmpty: "Δε βρέθηκαν ενεργές συσκευές.",
  sessionsLoading: "Φόρτωση συσκευών…",
  sessionsRetry: "Δοκιμάστε ξανά",
  sessionsRevoke: "Αποσύνδεση",
  sessionsRevoking: "Αποσύνδεση…",
  sessionsMore: "Περισσότερες συσκευές",
} as const;

/**
 * Favorites — account-page tabs and the detail
 * heart toggle. tabProjects is a deliberate anglicism (same policy as
 * nav.projects); render it inside lang="en". The heart glyphs ♥/♡ are
 * decorative (aria-hidden) with catalog-owned aria-labels — the structural
 * glyph exemption.
 */
const favorites = {
  sectionTitle: "Αγαπημένα",
  tabsLabel: "Τύπος περιεχομένου",
  tabPosts: "Αναρτήσεις",
  tabProjects: "Projects",
  emptyPosts: "Δεν έχετε αγαπημένες αναρτήσεις ακόμα.",
  emptyProjects: "Δεν έχετε αγαπημένα projects ακόμα.",
  addAria: "Προσθήκη στα αγαπημένα",
  removeAria: "Αφαίρεση από τα αγαπημένα",
} as const;

/**
 * Content follows — the detail-page bell toggle. The two aria-labels name
 * the toggle state; the bell glyph is decorative (aria-hidden), same
 * policy as the favorite heart.
 */
const follows = {
  // The button aria-label when NOT following.
  followAria: "Ακολούθησε",
  // The button aria-label when following.
  unfollowAria: "Σταμάτα να ακολουθείς",
} as const;

/**
 * RFC 9457 problem type paths → Greek display messages.
 *
 * The API returns relative `type` URIs in problem responses (e.g.,
 * `/problems/auth/invalid-credentials`). These keys map each type to
 * a user-facing Greek message displayed in error banners.
 */
const problems = {
  "/problems/bad-request": "Μη έγκυρο αίτημα.",
  "/problems/auth/invalid-credentials": "Λάθος στοιχεία σύνδεσης.",
  "/problems/forbidden": "Δεν έχετε πρόσβαση.",
  "/problems/conflict": "Διένεξη — δοκιμάστε ξανά.",
  "/problems/rate-limited": "Πολλές προσπάθειες. Δοκιμάστε αργότερα.",
  "/problems/payload-too-large": "Το αίτημα είναι πολύ μεγάλο.",
  "/problems/validation": "Η υποβολή περιέχει σφάλματα.",
  "/problems/internal-error": "Σφάλμα διακομιστή. Δοκιμάστε αργότερα.",
  "/problems/not-found": "Δε βρέθηκε.",
  "/problems/unsupported-media-type": "Μη υποστηριζόμενος τύπος αρχείου.",
  "/problems/auth/already-authenticated": "Είστε ήδη συνδεδεμένος.",
  // The forced-change gate's problem type.
  "/problems/auth/password-change-required":
    "Πρέπει να αλλάξετε τον κωδικό σας για να συνεχίσετε.",
  // The last active super-admin guard.
  "/problems/auth/last-super-admin":
    "Δεν μπορείτε να αφαιρέσετε τον τελευταίο ενεργό υπερδιαχειριστή.",
  // The one generic reset-token outcome (no distinction —
  // the token is the identity).
  "/problems/auth/reset-token-invalid":
    "Ο σύνδεσμος επαναφοράς δεν είναι έγκυρος ή έχει λήξει.",
  // The one generic verification-token outcome.
  "/problems/auth/verification-token-invalid":
    "Ο σύνδεσμος επιβεβαίωσης δεν είναι έγκυρος ή έχει λήξει.",
  // The ETag precondition problems.
  "/problems/precondition-required":
    "Λείπει το αναγνωριστικό έκδοσης της ανάρτησης.",
  "/problems/precondition-failed":
    "Η ανάρτηση άλλαξε από άλλον χρήστη. Φορτώστε ξανά τη σελίδα.",
} as const satisfies Record<string, string>;

/**
 * Field-level violation codes → Greek display messages.
 *
 * The API returns per-field `code` values in 422 validation responses
 * (e.g., `required`, `maxLength`). These keys map each code to a
 * user-facing Greek message displayed next to the relevant form field.
 */
const violations = {
  required: "Απαιτείται.",
  maxLength: "Υπερβαίνει το μέγιστο μήκος.",
  minLength: "Δεν πληροί το ελάχιστο μήκος.",
  maxItems: "Υπερβαίνει το μέγιστο αριθμό στοιχείων.",
  alreadyTaken: "Χρησιμοποιείται ήδη.",
  invalidFormat: "Μη έγκυρη μορφή.",
  invalidValue: "Μη έγκυρη τιμή.",
  invalid: "Μη έγκυρη υποβολή.",
  outOfRange: "Εκτός επιτρεπόμενων ορίων.",
} as const satisfies Record<string, string>;

/** Page titles for document.title on SPA navigation. */
const titles = {
  home: "Αρχική",
  signIn: "Σύνδεση",
  register: "Εγγραφή",
  forgot: "Επαναφορά κωδικού",
  reset: "Νέος κωδικός",
  account: "Λογαριασμός",
  // Anglicism — see el.nav.projects.
  projects: "Projects",
  search: "Αναζήτηση",
  // Greek — the about page title is "Η ομάδα".
  about: "Η ομάδα",
  notFound: "Δε βρέθηκε",
  // Base title for /blog/* until the post loads and refines it.
  blogDetail: "Ανάρτηση",
  // Base title for /projects/* until the project loads and refines it.
  projectsDetail: "Projects",
  admin: "Διαχείριση",
  notifications: "Ειδοποιήσεις",
} as const;

/** Blog — public list + detail copy. */
const blog = {
  pageTitle: "Τελευταίες αναρτήσεις",
  latestBadge: "Νέο",
  empty: "Δεν υπάρχουν αναρτήσεις ακόμα.",
  // Detail: a masked post (404 problem) is a deliberate state —
  // different copy from the router fallback's page-not-found.
  notFound: "Η ανάρτηση δε βρέθηκε.",
  backToHome: "Επιστροφή στην αρχική",
  downloadsTitle: "Λήψεις",
  // Deliberate anglicisms — the Greek community uses the English terms for
  // download types (same policy as Online/Offline; owner ruling).
  magnet: "Magnet",
  torrent: "Torrent",
  published: "Δημοσιεύτηκε",
  updated: "Ενημερώθηκε",
} as const;

/** About page ("Η ομάδα") — static community page copy: the legacy text
 * with the owner-ruled deltas (six active members, the «Donate» label, the
 * intro typo fix).
 *
 * Link LABELS are deliberate anglicisms rendered inside lang="en" (the
 * brand names and the «Donate» label); the lowercase tracker label is a
 * loanword inside a Greek sentence and renders plain. The member line
 * carries PUBLIC NAMES ONLY: roles are account state and stay off the
 * page. */
const about = {
  pageTitle: "Είμαστε οι Sick-Fansubs!",
  pageSubtitle: "Είμαστε άρρωστοι με τα anime!",
  intro:
    "Η ομάδα δημιουργήθηκε το 2011 κι από τότε εξακολουθούμε να έχουμε το ίδιο πάθος (αλλά περιορισμένο χρόνο :D ) για να σας προσφέρουμε τις αγαπημένες μας σειρές και ταινίες anime στην καλύτερη ποιότητα εικόνας και μετάφρασης.",
  membersLabel: "Ενεργοί συντελεστές:",
  members:
    "Kushoyarou, Katakuri, Medusa, Feitan, DarkClaw86, Akagami no Shanks.",
  // Each community link renders as one sentence with the label as the
  // inline anchor — prefix + label + suffix keep the anchor inside it.
  facebookPrefix:
    "Για να παρακολουθείς τα νέα και τις εξελίξεις μας, μπορείς να μας βρεις στο ",
  facebookLabel: "Facebook",
  facebookSuffix: ".",
  discordPrefix:
    "Για να συμμετέχεις στις συζητήσεις μας, να γίνεις μέλος της ομάδας ή αν έχεις τις γνώσεις και μπορείς να εμπλακείς στη διαδικασία της «δημιουργίας» των επεισοδίων (μετάφραση, χρονισμός, στοιχειοθετήσεις, καραόκε κ.ά.), μπορείς να μας βρεις στο ",
  discordLabel: "Discord",
  discordSuffix: ".",
  githubPrefix:
    "Είσαι προγραμματιστής ή έχεις κάποια ιδέα που μπορεί να κάνει τη σελίδα μας καλύτερη; Μπορείς να βρεις το αποθετήριο της σελίδας στο ",
  githubLabel: "Github",
  githubSuffix: ".",
  trackerPrefix:
    "Αν θέλεις να δεις όλη τη δουλειά της ομάδας μας, μπορείς να την βρεις και στον ",
  // Legacy copy renders the word lowercase inside the sentence
  // ("…στον tracker μας.") — kept verbatim per owner decision.
  trackerLabel: "tracker",
  trackerSuffix: " μας.",
  donatePrefix:
    "Αν θέλεις να στηρίξεις οικονομικά την προσπάθεια της ομάδας μας, μπορείς να το κάνεις μέσω ",
  donateLabel: "Donate",
  donateSuffix: ".",
} as const;

/** Donation copy (owner ruling) — a slim banner under the header + a
 * support line in the footer. The link label reuses el.about.donateLabel
 * (an anglicism rendered `lang="en"`); the banner sentence leads into the
 * link directly. */
const donation = {
  bannerText: "Η στήριξή σας βοηθά ενεργά την ομάδα!",
  supportText: "Στηρίξτε την ομάδα —",
} as const;

/** Install affordance — the control shared by the home donation strip
 * and the account page's profile panel. Android
 * and desktop Chrome hand the page a deferred install prompt; iOS never
 * does, so the control states the manual path instead. The iOS wording
 * reuses the «αρχική οθόνη» phrase from el.notifications.pushIOSHint. */
const install = {
  button: "Εγκατάσταση",
  iosHint:
    "Σε iPhone ή iPad: πάτησε «Κοινή χρήση» και μετά «Προσθήκη στην αρχική οθόνη».",
} as const;

/** Offline banner — the strip the shell
 * shows under the header while the browser reports no connection. The copy
 * states the state only — it never promises that cached pages load (the
 * service worker is production-only and its page cache is partial). */
const offline = {
  message: "Είσαι εκτός σύνδεσης. Κάποιες σελίδες μπορεί να μην φορτώσουν.",
} as const;

/** Projects — public list + detail copy.
 *
 * Several labels deliberately reuse the blog group's identical copy
 * instead of duplicating it: el.blog.published, el.blog.updated,
 * el.blog.magnet/torrent, and el.blog.backToHome. The ❯❯ details
 * aria-label is el.ui.details and the pager labels are the el.ui pager
 * keys; the meta lines never prefix the creator's name with «Από».
 */
const projects = {
  // The community's word for this entity is the English "projects"
  // (owner ruling — no Greek translation). Inside a Greek sentence it is a
  // loanword rendered plain; a whole-value label renders inside lang="en"
  // (the nav.projects policy).
  // The list page's screen-reader-only h1 (the grid is the visual entry,
  // same landmark pattern as the blog home's el.blog.pageTitle).
  projectsTitle: "Projects",
  projectsEmpty: "Δεν υπάρχουν projects ακόμα.",
  // Detail: a masked project (404 problem) is a deliberate
  // state — different copy from the router fallback's page-not-found.
  projectsNotFound: "Το project δε βρέθηκε.",
  projectsDownloadsTitle: "Λήψεις",
} as const;

/** Search — public search page copy.
 *
 * typeProjects is a deliberate anglicism (same policy as nav.projects:
 * the community uses "Projects"; render it inside lang="en"). The input
 * label reuses el.nav.search; the SUBMIT control has its own key (owner
 * ruling — it names the form action, not the page). Pagination is
 * URL-carried cursor paging; its labels live in el.ui so the shared
 * pager-nav serves every list. */
const search = {
  placeholder: "Αναζήτηση…",
  typeLabel: "Τύπος περιεχομένου",
  typeAll: "Όλα",
  typePosts: "Αναρτήσεις",
  typeProjects: "Projects",
  empty: "Δε βρέθηκαν αποτελέσματα.",
  prompt: "Πληκτρολόγησε μια λέξη ή φράση για αναζήτηση.",
  // aria-label for the input's clear (×) button.
  clear: "Καθαρισμός",
  // The form's submit control (owner ruling): the word the
  // legacy-tuned audience recognizes as "send this form", not a repeat of
  // the nav link's «Αναζήτηση».
  submit: "Υποβολή",
  // Sort control: label + the three orderings.
  sortLabel: "Ταξινόμηση",
  sortDate: "Νεότερα πρώτα",
  sortOldest: "Παλαιότερα πρώτα",
  sortTitle: "Αλφαβητικά (Α–Ω)",
  // Publication-date window.
  dateLabel: "Φίλτρο ημερομηνίας δημοσίευσης",
  dateFrom: "Από",
  dateTo: "Έως",
} as const;

/**
 * Admin — the content-administration surface: the media-upload widget, the
 * dashboard, and the create/edit form.
 */
const admin = {
  uploadChoose: "Επιλογή εικόνας",
  uploadReplace: "Αλλαγή εικόνας",
  uploadTooLarge: "Το αρχείο είναι πολύ μεγάλο.",
  uploadInvalid: "Μη έγκυρη μορφή εικόνας.",
  uploadRemove: "Αφαίρεση",
  // The upload-state line — the widget uploads on file
  // choice, so the line is what separates "πέρασες ένα αρχείο" from "η
  // αποθήκευση θα στείλει ΑΥΤΗ την εικόνα".
  uploadStatusNone: "Καμία εικόνα δεν έχει ανέβει.",
  uploadStatusUploading: "Η εικόνα ανεβαίνει…",
  uploadStatusUploaded: "Η εικόνα ανέβηκε και θα χρησιμοποιηθεί.",
  // The save gate while an upload is in flight: the value still
  // holds the PREVIOUS thumbnail, so a save now would write the wrong file.
  uploadInFlight:
    "Η εικόνα ανεβαίνει ακόμη. Περιμένετε να ολοκληρωθεί πριν αποθηκεύσετε.",
  // Dashboard.
  dashboardTitle: "Διαχείριση περιεχομένου",
  tabsLabel: "Τύπος περιεχομένου",
  contentTab: "Αναρτήσεις",
  // Deliberate anglicism — the same policy as nav.projects (the community
  // uses "Projects"); render it inside lang="en".
  projectsTab: "Projects",
  createPost: "Νέα ανάρτηση",
  createProject: "Νέο project",
  edit: "Επεξεργασία",
  listEmpty: "Δεν υπάρχουν αναρτήσεις ακόμα.",
  listEmptyProjects: "Δεν υπάρχουν projects ακόμα.",
  // Content-list filters. The three status keys below are
  // reused by the select's options — one source of truth for a status word.
  filtersLabel: "Φίλτρα περιεχομένου",
  filterSearchLabel: "Τίτλος",
  filterSearchPlaceholder: "Αναζήτηση τίτλου…",
  filterApply: "Εφαρμογή",
  listEmptyFiltered: "Δε βρέθηκε περιεχόμενο με αυτά τα φίλτρα.",
  statusDraft: "Πρόχειρο",
  statusPublished: "Δημοσιευμένη",
  statusArchived: "Αρχειοθετημένη",
  // Users tab.
  usersTab: "Μέλη",
  usersTitle: "Διαχείριση μελών",
  usersEmpty: "Δε βρέθηκαν μέλη.",
  usersFiltersLabel: "Φίλτρα μελών",
  filterRoleLabel: "Ρόλος",
  filterStatusLabel: "Κατάσταση",
  filterRoleAll: "Όλοι οι ρόλοι",
  filterStatusAll: "Όλες οι καταστάσεις",
  statusActive: "Ενεργό",
  statusSuspended: "Σε αναστολή",
  // Users-tab email verification status.
  emailVerified: "Email επιβεβαιωμένο",
  emailUnverified: "Email χωρίς επιβεβαίωση",
  // Users-tab password reset.
  resetButton: "Επαναφορά κωδικού",
  resetPending: "Επαναφορά…",
  resetConfirm:
    "Θα οριστεί νέος προσωρινός κωδικός και ο χρήστης θα πρέπει να τον αλλάξει στην επόμενη σύνδεση. Συνέχεια;",
  resetResultTitle: "Νέος προσωρινός κωδικός",
  resetResultIntro:
    "Παραδώστε τον κωδικό στον χρήστη με ασφαλή τρόπο. Εμφανίζεται μόνο μία φορά.",
  resetPasswordLabel: "Προσωρινός κωδικός",
  resetCopy: "Αντιγραφή",
  resetCopied: "Αντιγράφηκε!",
  resetClose: "Κλείσιμο",
  // Users-tab role change + deletion.
  roleSelectLabel: "Αλλαγή ρόλου",
  deleteUserConfirm:
    "Ο λογαριασμός θα διαγραφεί οριστικά. Το περιεχόμενό του παραμένει στην ιστοσελίδα. Συνέχεια;",
  // Users-tab suspension + reactivation. The
  // confirmation belongs to the STAGED save:
  // suspending is what needs a confirmation, and it is also what logs the
  // target out, so the panel asks at the moment the save would suspend.
  suspendConfirm:
    "Ο λογαριασμός θα ανασταλεί: ο χρήστης θα αποσυνδεθεί και δεν θα μπορεί να συνδεθεί μέχρι να ενεργοποιηθεί ξανά. Συνέχεια;",
  // Member card: the panel's one save, its cancel,
  // the staged-value summary, and the icon-only action group.
  memberSave: "Αποθήκευση",
  memberSavePending: "Αποθήκευση…",
  memberCancel: "Άκυρο",
  memberNoChange: "Καμία αλλαγή.",
  memberLeaveHint:
    "Οι μη αποθηκευμένες αλλαγές χάνονται αν κλείσετε τον πίνακα ή φύγετε από τη σελίδα.",
  memberActionsLabel: "Ενέργειες μέλους",
  // Metrics tab — the dashboard's statistics
  // surface. The status/account row labels reuse the existing status* keys
  // declared above (one source of truth).
  metricsTab: "Στατιστικά",
  metricsTotalsTitle: "Σύνολα",
  // What the bars MEAN, stated once per section: the
  // totals groups fill by share of their group, so every row also prints its
  // percentage; the activity group's bars are relative to the largest counter
  // in the section, where a percentage would be meaningless.
  metricsTotalsScale:
    "Οι ράβδοι και τα ποσοστά είναι μερίδια του συνόλου κάθε ομάδας.",
  metricsActivityScale:
    "Οι ράβδοι είναι σχετικές με τη μεγαλύτερη τιμή της ενότητας, όχι ποσοστά του συνόλου.",
  metricsActivityTitle: "Τελευταίες 30 ημέρες",
  metricsSincePrefix: "Από",
  metricsUsers: "Μέλη",
  // Deliberate anglicism — the nav.projects policy; rendered inside lang="en".
  metricsProjects: "Projects",
  metricsUsersByRole: "Μέλη ανά ρόλο",
  metricsUsersByStatus: "Μέλη ανά κατάσταση",
  metricsBlogPosts: "Αναρτήσεις",
  metricsComments: "Σχόλια",
  metricsFavorites: "Αγαπημένα",
  metricsRegistrations: "Εγγραφές",
  metricsActiveUsers: "Ενεργά μέλη",
  metricsSignInFailures: "Αποτυχημένες συνδέσεις",
  metricsContentCreated: "Νέο περιεχόμενο",
  metricsContentUpdated: "Ενημερώσεις περιεχομένου",
  metricsContentDeleted: "Διαγραφές περιεχομένου",
  metricsCommentDeletions: "Διαγραφές σχολίων",
  metricsUsersSuspended: "Αναστολές μελών",
  metricsUsersReactivated: "Ενεργοποιήσεις μελών",
  metricsUsersDeleted: "Διαγραφές μελών",
  metricsRoleChanges: "Αλλαγές ρόλων",
  // Logs tab — the super-admin log viewer. Level
  // names are wire values (debug/info/warn/error), rendered inside lang="en"
  // like the other technical anglicisms.
  logsTab: "Καταγραφές",
  logsSectionTitle: "Αρχεία καταγραφής",
  logsLevelLabel: "Ελάχιστο επίπεδο",
  logsQueryLabel: "Αναζήτηση",
  logsLimitLabel: "Πλήθος",
  logsApply: "Εφαρμογή",
  logsHint:
    "Εμφανίζονται οι πιο πρόσφατες καταγραφές που ταιριάζουν — χωρίς σελιδοποίηση.",
  logsEmpty: "Δε βρέθηκαν καταγραφές.",
  logsTableCaption: "Πίνακας καταγραφών",
  // English column header (owner ruling), rendered inside lang="en" like
  // the level values.
  logsTimeColumn: "timestamp",
  logsLevelColumn: "Επίπεδο",
  logsMessageColumn: "Μήνυμα",
  logsFieldsColumn: "Στοιχεία",
  logsFieldsEmpty: "—",
  // Audit browser — the second section of the
  // logs tab. Event names are wire values; the labels below are the Greek
  // reading of each event, keyed by the exact wire spelling. This is the only
  // NESTED catalog group (el.problems is flat) — the completeness helper in
  // el.test.ts recurses into it, and audit-api.test.ts asserts every accepted
  // event has a label here. An unknown key falls back to the wire value.
  auditTitle: "Αρχείο ελέγχου",
  auditEventLabel: "Τύπος συμβάντος",
  auditEventAll: "Όλοι οι τύποι",
  auditLimitLabel: "Πλήθος",
  auditApply: "Εφαρμογή",
  auditHint: "Εμφανίζονται τα πιο πρόσφατα συμβάντα — χωρίς σελιδοποίηση.",
  auditEmpty: "Δε βρέθηκαν συμβάντα.",
  auditTableCaption: "Πίνακας συμβάντων ελέγχου",
  // English headers (owner ruling), rendered inside lang="en" like the
  // level values. The id columns name the VALUE KIND — the cells
  // hold opaque identifiers (user ids for actor/target, the request id for
  // the request column) — and the address column says what its value is too.
  auditTimeColumn: "timestamp",
  auditEventColumn: "Συμβάν",
  auditResultColumn: "Αποτέλεσμα",
  auditActorColumn: "Actor (ID)",
  auditTargetColumn: "Target (ID)",
  auditRequestColumn: "Request (ID)",
  auditAddrColumn: "Address",
  auditNone: "—",
  auditEventNames: {
    sign_in_success: "Επιτυχής σύνδεση",
    sign_in_failure: "Αποτυχημένη σύνδεση",
    sign_out: "Αποσύνδεση",
    sign_out_all: "Αποσύνδεση από όλες τις συσκευές",
    password_changed: "Αλλαγή κωδικού",
    password_reset: "Επαναφορά κωδικού από διαχειριστή",
    role_changed: "Αλλαγή ρόλου",
    user_suspended: "Αναστολή μέλους",
    user_reactivated: "Ενεργοποίηση μέλους",
    user_deleted: "Διαγραφή μέλους",
    content_created: "Δημιουργία περιεχομένου",
    content_updated: "Ενημέρωση περιεχομένου",
    content_deleted: "Διαγραφή περιεχομένου",
    comment_deleted: "Διαγραφή σχολίου",
    password_reset_requested: "Αίτημα επαναφοράς κωδικού",
    password_reset_completed: "Ολοκλήρωση επαναφοράς κωδικού",
    email_verified: "Επιβεβαίωση email",
    email_changed: "Αλλαγή email",
    session_revoked: "Ανάκληση συνεδρίας",
  },
  // Create/edit form.
  formTitleCreate: "Νέα ανάρτηση",
  formTitleEdit: "Επεξεργασία ανάρτησης",
  formTitleCreateProject: "Νέο project",
  formTitleEditProject: "Επεξεργασία project",
  titleLabel: "Τίτλος",
  subtitleLabel: "Υπότιτλος",
  descriptionLabel: "Περιγραφή",
  // Technical anglicism — no natural Greek equivalent (same policy as
  // nav.projects); rendered inside lang="en".
  slugLabel: "Slug",
  slugHint: "Μικρά λατινικά γράμματα, ψηφία και ενωτικά.",
  thumbnailLabel: "Μικρογραφία",
  // The action set: each button names the state it
  // produces, so the form never asks the editor to work a select out.
  // Αποθήκευση keeps whatever state the row has (create: it lands as a
  // draft); Δημοσίευση publishes; Απόσυρση pulls a published row back to
  // draft; Αρχειοθέτηση takes it off the public site without deleting it.
  actionSaveDraft: "Αποθήκευση ως πρόχειρο",
  actionSave: "Αποθήκευση",
  actionSavePending: "Αποθήκευση…",
  actionPublish: "Δημοσίευση",
  actionPublishPending: "Δημοσίευση…",
  actionUnpublish: "Απόσυρση",
  actionUnpublishPending: "Απόσυρση…",
  actionArchive: "Αρχειοθέτηση",
  actionArchivePending: "Αρχειοθέτηση…",
  // The live state chip beside the form title.
  stateChipPrefix: "Κατάσταση",
  deleteButton: "Διαγραφή",
  deleteConfirm: "Η διαγραφή είναι οριστική. Συνέχεια;",
  deletePending: "Διαγραφή…",
  saved: "Η ανάρτηση αποθηκεύτηκε.",
  deleted: "Η ανάρτηση διαγράφηκε.",
  savedProject: "Το project αποθηκεύτηκε.",
  deletedProject: "Το project διαγράφηκε.",
  staleProject: "Το project άλλαξε από άλλον χρήστη. Φορτώστε ξανά τη σελίδα.",
  formNotFound: "Η ανάρτηση δε βρέθηκε.",
  formNotFoundProject: "Το project δε βρέθηκε.",
  backToDashboard: "Επιστροφή στη διαχείριση",
  // Download rows.
  downloadsLabel: "Σύνδεσμοι λήψης",
  downloadsHint: "Τουλάχιστον ένας σύνδεσμος (magnet ή torrent) ανά σειρά.",
  downloadResolutionLabel: "Ποιότητα",
  downloadNameLabel: "Ονομασία",
  // Technical anglicisms — no natural Greek equivalent (the slugLabel
  // policy); rendered inside lang="en".
  downloadMagnetLabel: "Magnet",
  downloadTorrentLabel: "Torrent",
  downloadAddRow: "Προσθήκη σειράς",
  downloadRemoveRow: "Αφαίρεση",
  downloadMoveUp: "Μετακίνηση επάνω",
  downloadMoveDown: "Μετακίνηση κάτω",
} as const;

/**
 * Notifications feed — the bell + /notifications
 * page copy. The per-event verbs are phrased around the masculine noun
 * "ο χρήστης" ("the user account") so the sentence never needs to agree
 * with the person's grammatical gender, which the app does not track. The
 * 3rd-person past verb forms (έκανε/ανέστειλε/...) are gender-neutral.
 */
const notifications = {
  title: "Ειδοποιήσεις",
  bellAria: "Ειδοποιήσεις",
  empty: "Δεν υπάρχουν ειδοποιήσεις.",
  // The sr-only prefix that states an unread feed row's state.
  unreadRow: "Αδιάβαστη",
  markAll: "Σήμανση όλων ως αναγνωσμένων",
  markAllPending: "Σήμανση…",
  // Deletion: one entry at a time, every READ event row, or the whole feed.
  // Event rows are deleted; the five account events are ledger rows and are
  // DISMISSED for the viewer — the ledger itself keeps them.
  deleteOne: "Διαγραφή",
  // The icon-only delete: the glyph cannot spell the pending
  // state, so the accessible name / tooltip carries it while the request is
  // out (the clearReadPending pair).
  deleteOnePending: "Διαγραφή…",
  clearRead: "Διαγραφή όλων των αναγνωσμένων",
  clearReadPending: "Διαγραφή…",
  deleteAll: "Διαγραφή όλων",
  deleteAllPending: "Διαγραφή…",
  deleteAllConfirm: "Διαγραφή όλων των ειδοποιήσεων;",
  // The maintenance pseudo-actor (cmd/resetpassword) and any deleted actor
  // render as a system label (owner ruling: null actorUsername → system
  // label — the maintenance actor is the only no-row actor among the five
  // feed events in practice).
  systemActor: "Σύστημα",
  // A deleted/missing target renders a placeholder.
  missingTarget: "Διαγραμμένος λογαριασμός",
  verbPasswordReset: "έκανε επαναφορά κωδικού για τον χρήστη",
  verbRoleChanged: "άλλαξε τον ρόλο του χρήστη",
  verbUserSuspended: "ανέστειλε τον χρήστη",
  verbUserReactivated: "επανέφερε τον χρήστη",
  verbUserDeleted: "διέγραψε τον χρήστη",
  // Unified feed: the comment_reply item line —
  // "<actor> απάντησε στο σχόλιό σου στο <contentTitle>"; the title is
  // catalog copy fallback when the content is gone (NULL → generic label).
  verbCommentReply: "απάντησε στο σχόλιό σου στο",
  // The comment item line: "<actor> σχολίασε στο <contentTitle>" — the
  // same gender-free actor+verb pattern as verbCommentReply.
  verbComment: "σχολίασε στο",
  // The content_updated item line:
  // "<actor> ενημέρωσε το <contentTitle>".
  verbContentUpdated: "ενημέρωσε το",
  // The new_content item line:
  // "<actor> δημοσίευσε το <contentTitle>".
  verbNewContent: "δημοσίευσε το",
  // The draft_activity item line — the staff-only notice
  // about UNPUBLISHED content: "<actor> <verb> <contentTitle>", where the
  // verb is the draftAction the notice carries.
  verbDraftCreated: "δημιούργησε το πρόχειρο",
  verbDraftUpdated: "ενημέρωσε το πρόχειρο",
  verbDraftUnpublished: "απέσυρε το",
  // The heart item line (owner copy):
  // "Το σχόλιό σου στο <contentTitle> αρέσει σε <actor>" — the two halves
  // wrap the title. "σε" + the bare username (no στον/στην article) keeps
  // the sentence gender-free, the same rule the account verbs follow.
  verbHeartOpen: "Το σχόλιό σου στο ",
  verbHeartClose: " αρέσει σε ",
  // The comment_removed line: the ACTORLESS
  // notice a moderator's deletion emits — "Το σχόλιό σου στο <title>
  // αφαιρέθηκε." No actor element exists, by ruling (the moderator is never
  // named; the audit ledger holds the accountability record).
  verbCommentRemovedOpen: "Το σχόλιό σου στο ",
  verbCommentRemovedClose: " αφαιρέθηκε.",
  genericContent: "Περιεχόμενο",
  resultSuccess: "Επιτυχία",
  resultFailure: "Αποτυχία",
  // ── Web push settings (the account-page settings section) ───────────
  pushTitle: "Ειδοποιήσεις συσκευής",
  pushExplain: "Λάβε ειδοποιήσεις push σε αυτό το πρόγραμμα περιήγησης.",
  pushEnable: "Ενεργοποίηση ειδοποιήσεων",
  pushEnablePending: "Ενεργοποίηση…",
  pushDisable: "Απενεργοποίηση",
  pushDisablePending: "Απενεργοποίηση…",
  pushDenied:
    "Οι ειδοποιήσεις είναι μπλοκαρισμένες για αυτό το πρόγραμμα περιήγησης. Άλλαξέ το από τις ρυθμίσεις του.",
  pushFailure: "Αποτυχία ενεργοποίησης. Δοκίμασε ξανά.",
  // The unavailable state: shown
  // in place of the whole control set when the environment cannot push —
  // an unsupported browser, or no service-worker registration (which is
  // every Vite dev session). Phrased neutrally because it covers both.
  pushUnavailable:
    "Οι ειδοποιήσεις συσκευής δεν είναι διαθέσιμες σε αυτή τη συσκευή ή τον περιηγητή.",
  pushIOSHint:
    "Σε iPhone ή iPad οι ειδοποιήσεις λειτουργούν μόνο όταν η εφαρμογή είναι εγκατεστημένη στην αρχική οθόνη.",
  pushDialogTitle: "Ενεργοποίηση ειδοποιήσεων",
  pushDialogBody:
    "Θα λάβεις ειδοποιήσεις για αντιδράσεις, απαντήσεις, σχόλια σε περιεχόμενο που ακολουθείς, ενημερώσεις περιεχομένου, νέες δημοσιεύσεις και αφαιρέσεις των σχολίων σου. Το πρόγραμμα περιήγησης θα σε ρωτήσει για την άδεια στο επόμενο βήμα.",
  pushDialogConfirm: "Συνέχεια",
  pushDialogCancel: "Άκυρο",
  pushKindsLabel: "Είδη ειδοποιήσεων",
  // The kind form's one save control: the checkboxes are edits until this
  // is pressed.
  pushSave: "Αποθήκευση",
  pushSavePending: "Αποθήκευση…",
  pushKindHeart: "«Μου αρέσει» στα σχόλιά μου",
  pushKindReply: "Απαντήσεις στα σχόλιά μου",
  // The comment kind toggle.
  pushKindComment: "Νέα σχόλια σε περιεχόμενο που ακολουθείς",
  // The content_updated kind toggle.
  pushKindUpdated: "Ενημερώσεις περιεχομένου που ακολουθείς",
  // The new_content kind toggle.
  pushKindNewContent: "Νέες δημοσιεύσεις",
  // The comment_removed kind toggle: the author notice for a moderator's
  // removal of the user's own comment.
  pushKindRemoved: "Αφαίρεση των σχολίων μου",
  // The staff-only draft_activity kind toggle: notices about
  // a teammate's unpublished content. The server withholds the kind from the
  // preferences list below moderator, so no client-side role gate exists.
  pushKindDraft: "Δραστηριότητα σε μη δημοσιευμένο περιεχόμενο",

  // Self-test send: the button that proves the push channel works, its
  // result lines, and the notification the service worker renders (its
  // title is el.ui.siteName). The notification pair is duplicated in sw.js
  // and pinned equal by push-artifacts.test.ts.
  pushTestButton: "Δοκιμή ειδοποίησης",
  pushTestPending: "Αποστολή…",
  // The server-side outcome only: an accepted endpoint means the push
  // SERVICE took the message, so the line must not promise that a
  // notification appeared on screen.
  pushTestSent: "Η δοκιμή παραδόθηκε στην υπηρεσία push.",
  // The press reached nothing and the report named no row of THIS device: a
  // refusal or a removal of this device has its own line below, so this copy
  // never contradicts what the report said.
  pushTestNone: "Η δοκιμή δεν παραδόθηκε σε καμία συσκευή.",
  // The per-device lines: the report names this
  // device's own row, so the refusal can be stated where it happened instead
  // of hiding behind a healthy sibling device. Deliberately non-technical —
  // no status codes, no endpoints, no service names reach the reader; the
  // detail lives in the logs and the API response.
  pushTestDeviceRejected:
    "Η υπηρεσία push απέρριψε αυτή τη συσκευή, οπότε η ειδοποίηση δεν θα φτάσει. Δοκίμασε ξανά αργότερα ή ενεργοποίησε τις ειδοποιήσεις από τις ρυθμίσεις του προγράμματος περιήγησης.",
  // No response arrived at all (a transport failure, not a refusal) — the
  // same report, a different remedy, so it gets its own line.
  pushTestDeviceUnreachable:
    "Δεν ήταν εφικτή η επικοινωνία με την υπηρεσία push για αυτή τη συσκευή. Δοκίμασε ξανά σε λίγο.",
  pushTestDeviceRemoved:
    "Η συσκευή αυτή δεν είναι πλέον εγγεγραμμένη στην υπηρεσία push. Χρειάζεται νέα ενεργοποίηση των ειδοποιήσεων.",
  pushTestNotification:
    "Οι ειδοποιήσεις λειτουργούν! Αυτή είναι μια δοκιμαστική ειδοποίηση.",
} as const;

/**
 * The unread badge's accessible announcement, with the Greek singular for
 * exactly one unread event and the plural otherwise (the retryAfterMessage
 * precedent).
 */
export function unreadBadgeLabel(count: number): string {
  return count === 1
    ? "1 αδιάβαστη ειδοποίηση"
    : `${count} αδιάβαστες ειδοποιήσεις`;
}

/**
 * Content-indicator count labels: the indicators render
 * the bare number visually; the FULL Greek phrase — singular for exactly
 * one, plural otherwise — rides each indicator's aria-label (the
 * details-marker pattern: role="img" + catalog-owned label).
 */
export function commentCountLabel(count: number): string {
  return count === 1 ? "1 σχόλιο" : `${count} σχόλια`;
}

export function favoriteCountLabel(count: number): string {
  return count === 1 ? "1 αγαπημένο" : `${count} αγαπημένα`;
}

/**
 * Comments thread — the inline section on the
 * blog/project detail pages. Heart states render as the shared SVG pair
 * (icons.heart / icons.heartFilled) with catalog-owned aria-labels.
 */
const comments = {
  title: "Σχόλια",
  empty: "Γίνε ο πρώτος που θα σχολιάσει.",
  // The click-to-load CTA — the collapsed indicator's button; the thread
  // mounts only after it (or a deep link).
  sectionCta: "Μπες στη συζήτηση",
  sortLabel: "Ταξινόμηση σχολίων",
  sortTop: "Κορυφαία",
  sortNewest: "Νέα",
  sortOldest: "Παλαιότερα",
  composerPlaceholder: "Μοιράσου τη γνώμη σου…",
  composerLabel: "Το σχόλιό σου",
  submit: "Σχολίασε",
  submitPending: "Δημοσίευση…",
  signInPrompt: "Συνδέσου για να γράψεις σχόλιο.",
  signInLink: "Σύνδεση",
  reply: "Απάντηση",
  replyPlaceholder: "Γράψε μια απάντηση…",
  replyLabel: "Η απάντησή σου",
  replySubmit: "Απάντησε",
  replySubmitPending: "Αποστολή…",
  edit: "Επεξεργασία",
  editLabel: "Επεξεργασία σχολίου",
  save: "Αποθήκευση",
  savePending: "Αποθήκευση…",
  cancel: "Ακύρωση",
  delete: "Διαγραφή",
  // The icon-only delete: the glyph cannot spell the pending state,
  // so the accessible name / tooltip carries it while the request is out.
  deletePending: "Διαγραφή…",
  // A delete removes the whole subtree (hard cascade) — the
  // confirmation names the replies.
  deleteConfirm: "Θα διαγραφεί το σχόλιο και οι απαντήσεις του. Συνέχεια;",
  edited: "επεξεργάστηκε",
  staffBadge: "Ομάδα",
  // The deep-link arrival markers: the chip labels the fragment
  // target, the notice covers a deleted/gone target (the focus fallback).
  targetChip: "Στόχος ειδοποίησης",
  deletedNotice: "Το σχόλιο στο οποίο αναφέρεται ο σύνδεσμος διαγράφηκε.",
  // The load-more affordance is sort-aware: the next page's direction
  // differs per tab (newest → older, oldest → newer, top → lower-ranked).
  loadMoreTop: "Φόρτωσε περισσότερα σχόλια",
  loadMoreNewest: "Φόρτωσε παλαιότερα σχόλια",
  loadMoreOldest: "Φόρτωσε νεότερα σχόλια",
  // The reply-window append — a top-level item
  // ships its first 3 replies; the rest load in place, with the total in
  // parentheses (the thread-title convention).
  loadMoreReplies: "Φόρτωσε περισσότερες απαντήσεις",
  loadingMore: "Φόρτωση…",
  heartAddAria: "Μου αρέσει",
  heartRemoveAria: "Ανάκληση «Μου αρέσει»",
  // Emoticon glyph labels — the tokens
  // render as colored SVG, so assistive tech needs the meaning the glyph
  // replaced. Rendered as each glyph's `aria-label` (never aria-hidden).
  emoticonSmile: "χαμόγελο",
  emoticonGrin: "πλατύ χαμόγελο",
  emoticonFrown: "θλιμμένη φατσούλα",
  emoticonWink: "κλείσιμο ματιού",
  emoticonTongue: "γλώσσα έξω",
  emoticonHeart: "καρδιά",
  emoticonSurprised: "έκπληξη",
  emoticonUnsure: "αβεβαιότητα",
} as const;

/** The full Greek message catalog. */
const meta = {
  // Static-document metadata shared by index.html and the PWA manifest:
  // static files cannot import the TS catalog, so the
  // catalog holds the value and pwa-artifacts.test.ts pins the duplicates
  // equal — drift fails the test instead of shipping.
  description: "Sick-Fansubs — ελληνική κοινότητα fansubbing",
} as const;

export const el = {
  ui,
  nav,
  auth,
  account,
  favorites,
  follows,
  blog,
  about,
  donation,
  install,
  offline,
  projects,
  search,
  problems,
  violations,
  titles,
  admin,
  notifications,
  comments,
  meta,
} as const;

/**
 * Looks up a problem type in the catalog and returns the Greek message.
 * Unknown types never reach the UI as raw paths — they fall back to the
 * generic Greek error and are logged for the developer.
 */
export function problemMessage(type: string): string {
  const known = (el.problems as Record<string, string>)[type];
  if (known) return known;
  console.warn("catalog: no Greek message for problem type", type);
  return el.ui.error;
}

/**
 * Looks up a violation code in the catalog and returns the Greek message.
 * Unknown codes fall back to the generic Greek error (never a raw code).
 */
export function violationMessage(code: string): string {
  const known = (el.violations as Record<string, string>)[code];
  if (known) return known;
  console.warn("catalog: no Greek message for violation code", code);
  return el.ui.error;
}

/**
 * Retry hint for rate-limited responses — rendered after the rate-limited
 * banner when the server's Retry-After header is available (the contract
 * requires the header; the frontend surfaces it instead of dropping it).
 */
export function retryAfterMessage(seconds: number): string {
  // Greek singular for exactly 1 second; 0 and 2+ take the plural.
  return seconds === 1
    ? "Δοκιμάστε ξανά σε 1 δευτερόλεπτο."
    : `Δοκιμάστε ξανά σε ${seconds} δευτερόλεπτα.`;
}
