import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';

import { ApiBlogPostController } from './api-blog-post.controller';
import { ApiBlogPostService } from './api-blog-post.service';
import { BlogPost, BlogPostSchema } from './schemas/blog-post.schema';

const BlogPostFeature = MongooseModule.forFeatureAsync([
  {
    name: BlogPost.name,
    useFactory: () => {
      return BlogPostSchema;
    },
  },
]);

@Module({
  imports: [BlogPostFeature],
  controllers: [ApiBlogPostController],
  providers: [ApiBlogPostService],
  exports: [BlogPostFeature, ApiBlogPostService],
})
export class ApiBlogPostModule {}
