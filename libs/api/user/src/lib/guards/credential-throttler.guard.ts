import { Injectable } from '@nestjs/common';
import { ThrottlerGuard } from '@nestjs/throttler';

@Injectable()
export class CredentialThrottlerGuard extends ThrottlerGuard {
  protected async getTracker(req: Record<string, unknown>): Promise<string> {
    const body = (req['body'] as Record<string, unknown> | undefined) ?? {};
    const username = typeof body['username'] === 'string' ? body['username'].trim().toLowerCase() : '';
    const email = typeof body['email'] === 'string' ? body['email'].trim().toLowerCase() : '';
    const identity = username || email || 'anonymous';

    const ip =
      (typeof req['ip'] === 'string' && req['ip']) ||
      (Array.isArray(req['ips']) && typeof req['ips'][0] === 'string' ? req['ips'][0] : '') ||
      (typeof req['socket'] === 'object' && req['socket'] !== null
        ? ((req['socket'] as Record<string, unknown>)['remoteAddress'] as string | undefined)
        : undefined) ||
      'unknown';

    return `${ip}:${identity}`;
  }
}
