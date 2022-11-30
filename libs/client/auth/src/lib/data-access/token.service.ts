import { Injectable } from '@angular/core';

const JWT_TOKEN = 'token';

@Injectable({
    providedIn: 'root',
})
export class TokenService {
    setToken(token: string) {
        localStorage.setItem(JWT_TOKEN, token);
    }

    getToken(): string {
        const token = localStorage.getItem(JWT_TOKEN);

        if (token) {
            return token;
        } else {
            return '';
        }
    }

    removeToken() {
        localStorage.removeItem(JWT_TOKEN);
    }

    isValidToken(): boolean {
        const token = this.getToken();

        if (token && token !== '') {
            const tokenDecode = JSON.parse(atob(token.split('.')[1]));

            return !this._tokenExpired(tokenDecode.exp);
        } else {
            return false;
        }
    }

    getUserIDFromToken(): string | null {
        const token = this.getToken();

        if (token && token !== '') {
            const tokenDecode = JSON.parse(atob(token.split('.')[1]));

            if (tokenDecode) {
                return tokenDecode.userId;
            } else {
                return null;
            }
        } else {
            return null;
        }
    }

    private _tokenExpired(expiration: string | number): boolean {
        return Math.floor(new Date().getTime() / 1000) >= +expiration;
    }
}
