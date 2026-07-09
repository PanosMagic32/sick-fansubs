import { AbstractControl, type ValidationErrors, type ValidatorFn } from '@angular/forms';

const HTTP_URL_PATTERN = /^https?:\/\//i;
const MAGNET_URL_PATTERN = /^magnet:\?xt=/i;

export function isValidBatchTorrentUrl(value: string): boolean {
  const normalized = value.trim();
  if (!normalized) return false;

  return HTTP_URL_PATTERN.test(normalized);
}

export function isValidBatchMagnetUrl(value: string): boolean {
  const normalized = value.trim();
  if (!normalized) return false;

  return MAGNET_URL_PATTERN.test(normalized);
}

export function batchTorrentUrlValidator(): ValidatorFn {
  return (control: AbstractControl): ValidationErrors | null => {
    const value = String(control.value ?? '');
    if (!value.trim()) return null;

    return isValidBatchTorrentUrl(value) ? null : { invalidBatchTorrentUrl: true };
  };
}

export function batchMagnetUrlValidator(): ValidatorFn {
  return (control: AbstractControl): ValidationErrors | null => {
    const value = String(control.value ?? '');
    if (!value.trim()) return null;

    return isValidBatchMagnetUrl(value) ? null : { invalidBatchMagnetUrl: true };
  };
}

export function atLeastOneResolutionForBatch(form: AbstractControl): ValidationErrors | null {
  const dl = String(form.get('downloadLink')?.value ?? '').trim();
  const dlTorrent = String(form.get('downloadLinkTorrent')?.value ?? '').trim();
  const dl4k = String(form.get('downloadLink4k')?.value ?? '').trim();
  const dl4kTorrent = String(form.get('downloadLink4kTorrent')?.value ?? '').trim();

  const has1080p = dl.length > 0 && dlTorrent.length > 0;
  const has2160p = dl4k.length > 0 && dl4kTorrent.length > 0;

  return has1080p || has2160p ? null : { atLeastOneResolution: true };
}
