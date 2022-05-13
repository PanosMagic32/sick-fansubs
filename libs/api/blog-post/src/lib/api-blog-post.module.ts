import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';

import { ApiBlogPostController } from './api-blog-post.controller';
import { ApiBlogPostService } from './api-blog-post.service';
import { BlogPostSchema } from './schemas/blog-post.schema';

@Module({
  imports: [MongooseModule.forFeature([{ name: 'BlogPost', schema: BlogPostSchema }])],
  controllers: [ApiBlogPostController],
  providers: [ApiBlogPostService],
})
export class ApiBlogPostModule {}
