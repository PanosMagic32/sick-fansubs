# libs/api/auth — `@api/auth`

## Purpose

JWT-based authentication for the NestJS API. Handles login, token generation, Passport strategy registration, and route guards.

## Path Alias

`@api/auth` → `libs/api/auth/src/index.ts`

## Public API (Exports)

| Export              | Description                                    |
| ------------------- | ---------------------------------------------- |
| `ApiAuthModule`     | NestJS module — import in `AppModule`          |
| `ApiAuthController` | `POST /api/auth/login`                         |
| `JwtAuthGuard`      | Protects routes requiring a valid Bearer token |

## Key Files

### Module

`src/lib/api-auth.module.ts`

- Imports `ApiUserModule` (needs `UserService`)
- Registers `JwtModule.registerAsync` — secret from `JWT_SECRET` env, expiration from `JWT_EXPIRATION` env
- Provides `ApiAuthService`, registers `JwtStrategy`

### Controller

`src/lib/api-auth.controller.ts`

- `POST /api/auth/login` — accepts `LoginUserDto` (`{ username, password }`), returns `{ username, accessToken }`

### Service

`src/lib/api-auth.service.ts`

- `validateUser(username, password)` — fetches user, bcrypt-compares password
- `generateJwt(user)` — signs `{ sub, username, email, isAdmin }` payload
- `login(loginUserDto)` — validates → signs → returns token
- `verifyJwt(token)` — verifies a JWT string

### Guards

`src/lib/guards/jwt.guard.ts` — re-exports `JwtAuthGuard` from `@api/user`
`src/lib/guards/local.guard.ts` — `LocalAuthGuard extends AuthGuard('local')` (declared, not active in controllers)

### Strategy

`src/lib/strategies/jwt.strategy.ts` — `PassportStrategy(Strategy)`

- Extracts Bearer token from `Authorization` header
- Validates with `JWT_SECRET`
- Returns decoded `{ sub, username, email, isAdmin }` payload injected as `req.user`

## Dependencies

- `@api/user` — `UserService` (for user lookup), `LoginUserDto`

## Nx Tasks

```bash
pnpm nx lint api-auth
pnpm nx test api-auth
```

## Notes

- `JwtAuthGuard` is defined in `@api/user` and re-exported here to keep a single guard implementation
- `LocalAuthGuard` exists but is not wired to any controller endpoint
