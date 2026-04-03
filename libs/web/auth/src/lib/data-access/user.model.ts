/* eslint-disable no-underscore-dangle */
import type { UserRole, UserStatus } from '@shared/types';

export class User {
  constructor(
    public username: string,
    public email?: string,
    public id?: string,
    public avatar?: string,
    private _role?: UserRole,
    private _status?: UserStatus,
    private _password?: string,
    private _accessToken?: string,
    private _oneSignalId?: string,
    private _pushId?: string,
    private _pushToken?: string,
  ) {}

  get role() {
    return this._role;
  }

  get status() {
    return this._status;
  }

  get accessToken() {
    return this._accessToken;
  }

  get password() {
    return this._password;
  }

  get oneSignalId() {
    return this._oneSignalId;
  }

  get pushId() {
    return this._pushId;
  }

  get pushToken() {
    return this._pushToken;
  }
}
