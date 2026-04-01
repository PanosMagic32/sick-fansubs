import { AbstractControl, type ValidationErrors, type ValidatorFn } from '@angular/forms';

const HTTP_URL_PATTERN = /^https?:\/\//i;
const MAGNET_URL_PATTERN = /^magnet:\?xt=/i;

export function isValidBatchTorrentUrl(value: string): boolean {
  const normalized = value.trim();
  if (!normalized) {
    return false;
  }

  return HTTP_URL_PATTERN.test(normalized);
}

export function isValidBatchMagnetUrl(value: string): boolean {
  const normalized = value.trim();
  if (!normalized) {
    return false;
  }

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
    if (!value.trim()) {
      return null;
    }

    return isValidBatchMagnetUrl(value) ? null : { invalidBatchMagnetUrl: true };
  };
}
