import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';
import { ConfigModule } from '@nestjs/config';

import { ApiBlogPostModule } from '@sick/api/blog-post';
import { ApiUserModule } from '@sick/api/user';
import { ApiProjectModule } from '@sick/api/project';
import { ApiAuthModule } from '@sick/api/auth';

@Module({
  imports: [
    ApiBlogPostModule,
    ApiUserModule,
    ApiProjectModule,
    ApiAuthModule,
    ConfigModule.forRoot({
      isGlobal: true,
    }),
    MongooseModule.forRoot(process.env.DATABASE_URL),
  ],
})
export class AppModule {}
