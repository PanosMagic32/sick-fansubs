import { HttpException, HttpStatus, Injectable } from '@nestjs/common';
import { JwtService } from '@nestjs/jwt';
import * as bcrypt from 'bcrypt';

import { User, UserService } from '@sick/api/user';

@Injectable()
export class ApiAuthService {
  constructor(private readonly jwtService: JwtService, private readonly userService: UserService) {}

  async generateJwt(payload: { username: string; email: string; isAdmin: boolean }): Promise<string> {
    return this.jwtService.signAsync(payload);
  }

  async hashPassword(password: string): Promise<string> {
    return bcrypt.hash(password, 12);
  }

  async comparePasswords(password: string, storedPasswordHash: string): Promise<boolean> {
    return bcrypt.compare(password, storedPasswordHash);
  }

  verifyJwt(jwt: string): Promise<any> {
    return this.jwtService.verifyAsync(jwt);
  }

  async validateUser(username: string, pass: string): Promise<any> {
    const user = await this.userService.findOneByUsername(username);

    if (user && this.comparePasswords(pass, user.password)) {
      const { password, avatar, ...result } = user;
      return result;
    }

    throw new HttpException('User not found', HttpStatus.NOT_FOUND);
  }

  async login(user: User) {
    const payload = await this.validateUser(user.username, user.password);
    const jwt = await this.generateJwt(payload);

    return {
      username: user.username,
      access_token: jwt,
    };
  }
}
