import { Module } from '@nestjs/common';
import { ConfigModule, ConfigService } from '@nestjs/config';
import { JwtModule } from '@nestjs/jwt';

import { ApiUserModule } from '@api/user';

import { ApiAuthController } from './api-auth.controller';
import { ApiAuthService } from './api-auth.service';
import { JwtStrategy } from './strategies/jwt.strategy';

@Module({
  imports: [
    ApiUserModule,
    JwtModule.registerAsync({
      imports: [ConfigModule],
      inject: [ConfigService],
      useFactory: async (configService: ConfigService) => ({
        secret: configService.get('JWT_SECRET'),
        signOptions: { expiresIn: configService.get('JWT_EXPIRATION_TIME') },
      }),
    }),
  ],
  controllers: [ApiAuthController],
  providers: [ApiAuthService, JwtStrategy],
})
export class ApiAuthModule {}
