import { Injectable } from '@angular/core';

@Injectable({
  providedIn: 'root',
})
export class ConfigService {
  private apiUrl: string | undefined = '';
  private appVersion: string | undefined = '';
  private facebookUrl: string | undefined = '';
  private discordUrl: string | undefined = '';
  private githubUrl: string | undefined = '';
  private paypalUrl: string | undefined = '';
  private trackerUrl: string | undefined = '';

  get API_URL() {
    return this.apiUrl;
  }

  set API_URL(apiUrl) {
    this.apiUrl = apiUrl;
  }

  get APP_VERSION() {
    return this.appVersion;
  }

  set APP_VERSION(appVersion) {
    this.appVersion = appVersion;
  }

  get FACEBOOK_URL() {
    return this.facebookUrl;
  }

  set FACEBOOK_URL(facebookUrl) {
    this.facebookUrl = facebookUrl;
  }

  get DISCORD_URL() {
    return this.discordUrl;
  }

  set DISCORD_URL(discordUrl) {
    this.discordUrl = discordUrl;
  }

  get GITHUB_URL() {
    return this.githubUrl;
  }

  set GITHUB_URL(githubUrl) {
    this.githubUrl = githubUrl;
  }

  get PAYPAL_URL() {
    return this.paypalUrl;
  }

  set PAYPAL_URL(paypalUrl) {
    this.paypalUrl = paypalUrl;
  }

  get TRACKER_URL() {
    return this.trackerUrl;
  }

  set TRACKER_URL(trackerUrl) {
    this.trackerUrl = trackerUrl;
  }
}
