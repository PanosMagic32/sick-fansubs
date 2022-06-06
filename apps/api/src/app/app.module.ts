import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';

import { ApiBlogPostModule } from '@sick/api/blog-post';
import { ApiUserModule } from '@sick/api/user';
import { ApiProjectModule } from '@sick/api/project';

const url = process.env.MONGO_URL || 'localhost';

@Module({
  imports: [ApiBlogPostModule, ApiUserModule, ApiProjectModule, MongooseModule.forRoot(`mongodb://${url}:27017`)],
})
export class AppModule {}
