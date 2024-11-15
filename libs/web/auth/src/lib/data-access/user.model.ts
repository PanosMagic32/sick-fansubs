/* eslint-disable no-underscore-dangle */
export class User {
  constructor(
    public username: string,
    public email?: string,
    public id?: string,
    public avatar?: string,
    private _isAdmin?: boolean,
    private _password?: string,
    private _accessToken?: string,
    private _oneSignalId?: string,
    private _pushId?: string,
    private _pushToken?: string,
  ) {}

  get isAdmin() {
    return this._isAdmin;
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
