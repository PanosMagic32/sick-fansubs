import { FormControl, FormGroup } from '@angular/forms';

import {
  atLeastOneResolutionForBatch,
  batchMagnetUrlValidator,
  batchTorrentUrlValidator,
  isValidBatchMagnetUrl,
  isValidBatchTorrentUrl,
} from './batch-link.validators';

describe('batch-link validators', () => {
  describe('isValidBatchTorrentUrl', () => {
    it('returns true for http URLs', () => {
      expect(isValidBatchTorrentUrl('http://example.com/file.torrent')).toBe(true);
    });

    it('returns true for https URLs', () => {
      expect(isValidBatchTorrentUrl('https://example.com/file.torrent')).toBe(true);
    });

    it('returns false for magnet URIs', () => {
      expect(isValidBatchTorrentUrl('magnet:?xt=urn:btih:abc123')).toBe(false);
    });

    it('returns false for empty strings', () => {
      expect(isValidBatchTorrentUrl('')).toBe(false);
    });

    it('returns false for whitespace-only strings', () => {
      expect(isValidBatchTorrentUrl('   ')).toBe(false);
    });
  });

  describe('isValidBatchMagnetUrl', () => {
    it('returns true for magnet URIs', () => {
      expect(isValidBatchMagnetUrl('magnet:?xt=urn:btih:abc123')).toBe(true);
    });

    it('returns true for magnet URIs with additional params', () => {
      expect(isValidBatchMagnetUrl('magnet:?xt=urn:btih:abc123&dn=test')).toBe(true);
    });

    it('returns false for http URLs', () => {
      expect(isValidBatchMagnetUrl('http://example.com/file.torrent')).toBe(false);
    });

    it('returns false for https URLs', () => {
      expect(isValidBatchMagnetUrl('https://example.com/file.torrent')).toBe(false);
    });

    it('returns false for empty strings', () => {
      expect(isValidBatchMagnetUrl('')).toBe(false);
    });

    it('returns false for whitespace-only strings', () => {
      expect(isValidBatchMagnetUrl('   ')).toBe(false);
    });
  });

  describe('batchTorrentUrlValidator', () => {
    it('returns null for empty control', () => {
      const control = new FormControl('');
      expect(batchTorrentUrlValidator()(control)).toBeNull();
    });

    it('returns null for whitespace-only control', () => {
      const control = new FormControl('   ');
      expect(batchTorrentUrlValidator()(control)).toBeNull();
    });

    it('returns null for valid http URL', () => {
      const control = new FormControl('https://example.com/file.torrent');
      expect(batchTorrentUrlValidator()(control)).toBeNull();
    });

    it('returns null for valid http URL (non-https)', () => {
      const control = new FormControl('http://example.com/file.torrent');
      expect(batchTorrentUrlValidator()(control)).toBeNull();
    });

    it('returns error for invalid value', () => {
      const control = new FormControl('not-a-url');
      expect(batchTorrentUrlValidator()(control)).toEqual({ invalidBatchTorrentUrl: true });
    });

    it('returns error for magnet URI', () => {
      const control = new FormControl('magnet:?xt=urn:btih:abc123');
      expect(batchTorrentUrlValidator()(control)).toEqual({ invalidBatchTorrentUrl: true });
    });

    it('handles null control value', () => {
      const control = new FormControl(null);
      expect(batchTorrentUrlValidator()(control)).toBeNull();
    });
  });

  describe('batchMagnetUrlValidator', () => {
    it('returns null for empty control', () => {
      const control = new FormControl('');
      expect(batchMagnetUrlValidator()(control)).toBeNull();
    });

    it('returns null for whitespace-only control', () => {
      const control = new FormControl('   ');
      expect(batchMagnetUrlValidator()(control)).toBeNull();
    });

    it('returns null for valid magnet URI', () => {
      const control = new FormControl('magnet:?xt=urn:btih:abc123');
      expect(batchMagnetUrlValidator()(control)).toBeNull();
    });

    it('returns error for http URL', () => {
      const control = new FormControl('https://example.com/file.torrent');
      expect(batchMagnetUrlValidator()(control)).toEqual({ invalidBatchMagnetUrl: true });
    });

    it('returns error for invalid value', () => {
      const control = new FormControl('not-a-magnet');
      expect(batchMagnetUrlValidator()(control)).toEqual({ invalidBatchMagnetUrl: true });
    });

    it('handles null control value', () => {
      const control = new FormControl(null);
      expect(batchMagnetUrlValidator()(control)).toBeNull();
    });
  });

  describe('atLeastOneResolutionForBatch', () => {
    it('returns null when 1080p pair is complete', () => {
      const form = new FormGroup({
        downloadLink: new FormControl('magnet:?xt=urn:btih:abc'),
        downloadLinkTorrent: new FormControl('https://example.com/file.torrent'),
        downloadLink4k: new FormControl(''),
        downloadLink4kTorrent: new FormControl(''),
      });
      expect(atLeastOneResolutionForBatch(form)).toBeNull();
    });

    it('returns null when 2160p pair is complete', () => {
      const form = new FormGroup({
        downloadLink: new FormControl(''),
        downloadLinkTorrent: new FormControl(''),
        downloadLink4k: new FormControl('magnet:?xt=urn:btih:4kabc'),
        downloadLink4kTorrent: new FormControl('https://example.com/file-4k.torrent'),
      });
      expect(atLeastOneResolutionForBatch(form)).toBeNull();
    });

    it('returns null when both resolution pairs are complete', () => {
      const form = new FormGroup({
        downloadLink: new FormControl('magnet:?xt=urn:btih:abc'),
        downloadLinkTorrent: new FormControl('https://example.com/file.torrent'),
        downloadLink4k: new FormControl('magnet:?xt=urn:btih:4kabc'),
        downloadLink4kTorrent: new FormControl('https://example.com/file-4k.torrent'),
      });
      expect(atLeastOneResolutionForBatch(form)).toBeNull();
    });

    it('returns error when neither resolution pair is complete', () => {
      const form = new FormGroup({
        downloadLink: new FormControl(''),
        downloadLinkTorrent: new FormControl(''),
        downloadLink4k: new FormControl(''),
        downloadLink4kTorrent: new FormControl(''),
      });
      expect(atLeastOneResolutionForBatch(form)).toEqual({ atLeastOneResolution: true });
    });

    it('returns error with partial 1080p pair (only downloadLink)', () => {
      const form = new FormGroup({
        downloadLink: new FormControl('magnet:?xt=urn:btih:abc'),
        downloadLinkTorrent: new FormControl(''),
        downloadLink4k: new FormControl(''),
        downloadLink4kTorrent: new FormControl(''),
      });
      expect(atLeastOneResolutionForBatch(form)).toEqual({ atLeastOneResolution: true });
    });

    it('returns error with partial 1080p pair (only downloadLinkTorrent)', () => {
      const form = new FormGroup({
        downloadLink: new FormControl(''),
        downloadLinkTorrent: new FormControl('https://example.com/file.torrent'),
        downloadLink4k: new FormControl(''),
        downloadLink4kTorrent: new FormControl(''),
      });
      expect(atLeastOneResolutionForBatch(form)).toEqual({ atLeastOneResolution: true });
    });

    it('handles whitespace-only strings as empty', () => {
      const form = new FormGroup({
        downloadLink: new FormControl('   '),
        downloadLinkTorrent: new FormControl('   '),
        downloadLink4k: new FormControl('   '),
        downloadLink4kTorrent: new FormControl('   '),
      });
      expect(atLeastOneResolutionForBatch(form)).toEqual({ atLeastOneResolution: true });
    });
  });
});
