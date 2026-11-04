/**
 * Community link constants — the target hardcodes these public values;
 * per-environment config is deferred future work.
 *
 * These are public community links, not secrets — safe to ship in the
 * frontend bundle.
 */
import { el } from "../catalog/el.js";

export const FACEBOOK_URL = "https://www.facebook.com/SickFansubs/";
export const DISCORD_URL = "https://discord.com/invite/28aCBqu";
export const GITHUB_URL = "https://github.com/PanosMagic32/sick-fansubs";
export const TRACKER_URL = "https://nyaa.si/user/Sick-Fansubs";
export const BUY_ME_A_COFFEE_URL = "https://www.buymeacoffee.com/sickfansubs";

export interface CommunityLink {
  /** Deliberate English brand label, rendered inside `lang="en"`. */
  label: string;
  url: string;
  /** Selects the shared brand token and the surface's tone class. */
  tone: "facebook" | "discord" | "github";
}

/**
 * The community destinations in display order. The about page's rows and
 * the footer's social row both derive from this list, so a URL or order
 * change lands in one place.
 */
export const COMMUNITY_LINKS: readonly CommunityLink[] = [
  { label: el.about.facebookLabel, url: FACEBOOK_URL, tone: "facebook" },
  { label: el.about.discordLabel, url: DISCORD_URL, tone: "discord" },
  { label: el.about.githubLabel, url: GITHUB_URL, tone: "github" },
];
