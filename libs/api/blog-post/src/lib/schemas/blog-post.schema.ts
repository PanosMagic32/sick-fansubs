import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import { Document } from 'mongoose';

export type BlogPostDocument = BlogPost & Document;

@Schema()
export class BlogPost {
  @Prop({ required: true })
  title!: string;

  @Prop({ required: true })
  subtitle!: string;

  @Prop({ required: true })
  description!: string;

  @Prop({ required: true })
  thumbnail!: string;

  @Prop({ required: true })
  downloadLink!: string;

  // @Prop({ type: Date })
  // dateTime!: Date;
}

export const BlogPostSchema = SchemaFactory.createForClass(BlogPost);
