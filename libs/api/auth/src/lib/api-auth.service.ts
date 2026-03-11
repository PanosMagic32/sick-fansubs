import { ForbiddenException, Injectable, NotFoundException } from '@nestjs/common';
import { JwtService } from '@nestjs/jwt';
import * as bcrypt from 'bcrypt';

import { LoginUserDto, UserService } from '@api/user';

@Injectable()
export class ApiAuthService {
  constructor(
    private readonly jwtService: JwtService,
    private readonly userService: UserService,
  ) {}

  async generateJwt(payload: { sub: string; username: string; email: string; isAdmin: boolean }): Promise<string> {
    return this.jwtService.signAsync(payload);
  }

  async comparePasswords(password: string, storedPasswordHash: string): Promise<boolean> {
    return bcrypt.compare(password, storedPasswordHash);
  }

  verifyJwt(jwt: string): Promise<{ sub: string; username: string; email: string; isAdmin: boolean }> {
    return this.jwtService.verifyAsync(jwt);
  }

  async validateUser(
    username: string,
    pass: string,
  ): Promise<{ sub: string; username: string; email: string; isAdmin: boolean }> {
    const user = await this.userService.findOneByUsername(username);

    if (user && (await this.comparePasswords(pass, user.password))) {
      return {
        sub: user._id.toString(),
        username: user.username,
        email: user.email,
        isAdmin: user.isAdmin,
      };
    }

    if (!user) throw new NotFoundException('User not found.');

    throw new ForbiddenException('Invalid user credentials.');
  }

  async login(loginUserDto: LoginUserDto) {
    const payload = await this.validateUser(loginUserDto.username, loginUserDto.password);
    const jwt = await this.generateJwt(payload);

    return {
      username: loginUserDto.username,
      accessToken: jwt,
    };
  }
}
