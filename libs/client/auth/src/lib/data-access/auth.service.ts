import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { Observable } from 'rxjs';

@Injectable({
    providedIn: 'root',
})
export class AuthService {
    constructor(private http: HttpClient) {}

    login(username: string, password: string): Observable<{ username: string; accessToken: string }> {
        return this.http.post<{ username: string; accessToken: string }>('/api/auth/login', { username, password });
    }

    signUp(
        username: string,
        email: string,
        password: string,
        avatar?: string
    ): Observable<{ id: string; username: string; email: string }> {
        return this.http.post<{ id: string; username: string; email: string; isAdmin: boolean }>('/api/user', {
            username,
            email,
            password,
            avatar,
        });
    }
}
